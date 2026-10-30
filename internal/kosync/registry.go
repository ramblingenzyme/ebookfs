package kosync

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// SidecarData is the structure stored in .sidecar/kosync.json
type SidecarData struct {
	DocumentIDs []string `json:"document_ids"`
	Progress    Progress `json:"progress"`
}

// docRegistry owns everything kosync keeps in .sidecar/kosync.json: the
// document_id → book_id index, progress read/write for the HTTP handler, and
// the sidecar writes behind the Hook events. The index is in-memory only and
// rebuilt from sidecars on every startup, so books that arrive outside the
// Hook event path still get document IDs and progress.
type docRegistry struct {
	mu   sync.RWMutex
	data map[string]int64 // document_id → book_id
}

func newDocRegistry() *docRegistry {
	return &docRegistry{data: make(map[string]int64)}
}

// Lookup returns the book_id for a document_id, or ok=false if not found.
func (r *docRegistry) Lookup(docID string) (int64, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	bookID, ok := r.data[docID]
	return bookID, ok
}

// Restore rebuilds the index from all book sidecars and creates missing
// sidecars. Runs unconditionally at startup; harmless when every sidecar is
// already present.
func (r *docRegistry) Restore(lib *library.Library) error {
	books, err := lib.Search(library.Query{})
	if err != nil {
		return fmt.Errorf("searching library: %w", err)
	}

	// Built without the lock; swapped in atomically at the end.
	newData := make(map[string]int64)
	for _, book := range books {
		raw, err := lib.ReadSidecar(book.ID(), "kosync.json")
		if errors.Is(err, fs.ErrNotExist) {
			if err := r.createSidecar(lib, book, newData); err != nil {
				slog.Error("kosync: failed to create sidecar during restore", "book_id", book.ID(), "error", err)
			}
			continue
		}
		if err != nil {
			slog.Warn("kosync: failed to read sidecar during restore", "book_id", book.ID(), "error", err)
			continue
		}

		var sidecar SidecarData
		if err := json.Unmarshal(raw, &sidecar); err != nil {
			slog.Warn("kosync: corrupt sidecar during restore", "book_id", book.ID(), "error", err)
			continue
		}

		for _, docID := range sidecar.DocumentIDs {
			if docID != "" {
				newData[docID] = book.ID()
				continue
			}
		}
	}

	r.mu.Lock()
	r.data = newData
	r.mu.Unlock()

	slog.Info("kosync: index restored", "books", len(books), "indexed", len(newData))
	return nil
}

// createSidecar computes the document ID for a book and writes its sidecar.
// It adds the index entry to the provided map.
func (r *docRegistry) createSidecar(lib *library.Library, book *library.Book, index map[string]int64) error {
	epub, err := lib.Content(book.ID())
	if err != nil {
		return fmt.Errorf("opening epub: %w", err)
	}
	defer epub.Close()

	docID, err := PartialMD5(epub, book.EpubSize())
	if err != nil {
		return fmt.Errorf("computing document ID: %w", err)
	}

	if err := lib.WithSidecars(book.ID(), func(root *os.Root) error {
		return writeSidecar(root, SidecarData{
			DocumentIDs: []string{docID},
			Progress:    Progress{},
		})
	}); err != nil {
		return fmt.Errorf("writing sidecar: %w", err)
	}

	index[docID] = book.ID()
	return nil
}

// Assign computes the document ID and initializes the sidecar (OnIngested).
func (r *docRegistry) Assign(book *library.Book, sidecars *os.Root, openEpub func() (library.EpubReader, error)) {
	docID, err := computeDocumentID(book, openEpub)
	if err != nil {
		slog.Error("kosync: failed to compute document ID", "book_id", book.ID(), "error", err)
		return
	}

	data := SidecarData{
		DocumentIDs: []string{docID},
		Progress:    Progress{},
	}
	if err := writeSidecar(sidecars, data); err != nil {
		slog.Error("kosync: failed to write sidecar", "book_id", book.ID(), "error", err)
		return
	}

	r.mu.Lock()
	r.data[docID] = book.ID()
	r.mu.Unlock()

	slog.Info("kosync: computed document ID", "book_id", book.ID(), "document_id", docID)
}

// Refresh recomputes the document ID and prepends it to the sidecar when it
// changed, preserving historical IDs so progress stays reachable (OnEdited).
func (r *docRegistry) Refresh(book *library.Book, sidecars *os.Root, openEpub func() (library.EpubReader, error)) {
	newDocID, err := computeDocumentID(book, openEpub)
	if err != nil {
		slog.Error("kosync: failed to compute document ID", "book_id", book.ID(), "error", err)
		return
	}

	data, err := readSidecar(sidecars)
	if err != nil {
		slog.Error("kosync: failed to read sidecar", "book_id", book.ID(), "error", err)
		return
	}

	if len(data.DocumentIDs) > 0 && data.DocumentIDs[0] == newDocID {
		return
	}

	data.DocumentIDs = append([]string{newDocID}, data.DocumentIDs...)
	if err := writeSidecar(sidecars, *data); err != nil {
		slog.Error("kosync: failed to write sidecar", "book_id", book.ID(), "error", err)
		return
	}

	r.mu.Lock()
	r.data[newDocID] = book.ID()
	r.mu.Unlock()

	slog.Info("kosync: updated document ID", "book_id", book.ID(), "document_id", newDocID)
}

// Forget drops every document ID the sidecar lists, before the book is
// deleted from disk (OnDeleting).
func (r *docRegistry) Forget(book *library.Book, sidecars *os.Root) {
	data, err := readSidecar(sidecars)
	if err != nil {
		slog.Error("kosync: failed to read sidecar for deletion", "book_id", book.ID(), "error", err)
		return
	}

	r.mu.Lock()
	for _, docID := range data.DocumentIDs {
		delete(r.data, docID)
	}
	r.mu.Unlock()

	slog.Info("kosync: removed document IDs from index", "book_id", book.ID(), "count", len(data.DocumentIDs))
}

// ReadProgress returns the stored progress for a document ID. found is false
// for an unknown document ID or a book with no sidecar yet.
func (r *docRegistry) ReadProgress(lib *library.Library, docID string) (progress Progress, found bool, err error) {
	bookID, ok := r.Lookup(docID)
	if !ok {
		return Progress{}, false, nil
	}

	raw, err := lib.ReadSidecar(bookID, "kosync.json")
	if errors.Is(err, fs.ErrNotExist) {
		return Progress{}, false, nil
	}
	if err != nil {
		return Progress{}, false, err
	}

	var sidecar SidecarData
	if err := json.Unmarshal(raw, &sidecar); err != nil {
		return Progress{}, false, err
	}
	return sidecar.Progress, true, nil
}

// StoreProgress writes progress into the book's sidecar under the per-book
// lock, returning the stored timestamp. found is false for an unknown
// document ID; the caller then stores nothing.
func (r *docRegistry) StoreProgress(lib *library.Library, docID string, progress Progress) (bookID, timestamp int64, found bool, err error) {
	bookID, ok := r.Lookup(docID)
	if !ok {
		return 0, 0, false, nil
	}

	err = lib.WithSidecars(bookID, func(root *os.Root) error {
		sidecar, err := readSidecar(root)
		if err != nil {
			return err
		}
		progress.Timestamp = time.Now().Unix()
		sidecar.Progress = progress
		timestamp = progress.Timestamp
		return writeSidecar(root, *sidecar)
	})
	if err != nil {
		return bookID, 0, true, err
	}
	return bookID, timestamp, true, nil
}

// snapshot returns a copy of the index, for tests.
func (r *docRegistry) snapshot() map[string]int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cp := make(map[string]int64, len(r.data))
	for k, v := range r.data {
		cp[k] = v
	}
	return cp
}

// ponytail: only the epub is hashed. With reader.convert the device holds the
// kepub, whose hash maps to nothing, so its progress is dropped. Map the
// kepub's hash too, refreshed on each reconversion, if convert and kosync are
// used together.
func computeDocumentID(book *library.Book, openEpub func() (library.EpubReader, error)) (string, error) {
	epub, err := openEpub()
	if err != nil {
		return "", err
	}
	defer epub.Close()

	return PartialMD5(epub, book.EpubSize())
}

func readSidecar(sidecars *os.Root) (*SidecarData, error) {
	f, err := sidecars.Open("kosync.json")
	if err != nil {
		if os.IsNotExist(err) {
			return &SidecarData{DocumentIDs: []string{}}, nil
		}
		return nil, err
	}
	defer f.Close()

	var data SidecarData
	if err := json.NewDecoder(f).Decode(&data); err != nil {
		return nil, err
	}
	return &data, nil
}

// writeSidecar renames into place. A truncated kosync.json fails to parse,
// which loses the progress and fails every later PUT for the book.
func writeSidecar(sidecars *os.Root, data SidecarData) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	const tmpName = ".kosync.json.tmp"
	if err := sidecars.WriteFile(tmpName, raw, 0644); err != nil {
		return err
	}
	return sidecars.Rename(tmpName, "kosync.json")
}

