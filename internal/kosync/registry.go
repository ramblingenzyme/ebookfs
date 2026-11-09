package kosync

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
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
		var docIDs []string
		err := lib.WithSidecars(book.ID(), func(root *os.Root) error {
			var err error
			docIDs, err = restoreDocumentIDs(lib, book.ID(), root)
			return err
		})
		if err != nil {
			slog.Error("kosync: failed to restore sidecar", "book_id", book.ID(), "error", err)
			continue
		}
		for _, docID := range docIDs {
			if docID != "" {
				newData[docID] = book.ID()
			}
		}
	}

	r.mu.Lock()
	r.data = newData
	r.mu.Unlock()

	slog.Info("kosync: index restored", "books", len(books), "indexed", len(newData))
	return nil
}

// restoreDocumentIDs runs under the per-book lock, creating a missing sidecar.
// The book is re-read there, since the Restore snapshot's size may predate an
// edit and would hash the wrong prefix of the file.
func restoreDocumentIDs(lib *library.Library, id int64, root *os.Root) ([]string, error) {
	raw, err := root.ReadFile("kosync.json")
	if errors.Is(err, fs.ErrNotExist) {
		book, err := lib.Get(id)
		if err != nil {
			return nil, err
		}
		docID, err := computeDocumentID(book, func() (library.EpubReader, error) { return lib.Content(id) })
		if err != nil {
			return nil, fmt.Errorf("computing document ID: %w", err)
		}
		if err := writeSidecar(root, SidecarData{DocumentIDs: []string{docID}}); err != nil {
			return nil, fmt.Errorf("writing sidecar: %w", err)
		}
		return []string{docID}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading sidecar: %w", err)
	}

	var sidecar SidecarData
	if err := json.Unmarshal(raw, &sidecar); err != nil {
		return nil, fmt.Errorf("corrupt sidecar: %w", err)
	}
	return sidecar.DocumentIDs, nil
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

// Forget drops every document ID mapped to bookID (OnDeleted). It waits for
// the delete to succeed, since a failed one leaves the book live and still
// syncing.
func (r *docRegistry) Forget(bookID int64) {
	r.mu.Lock()
	n := len(r.data)
	maps.DeleteFunc(r.data, func(_ string, id int64) bool { return id == bookID })
	n -= len(r.data)
	r.mu.Unlock()

	slog.Info("kosync: removed document IDs from index", "book_id", bookID, "count", n)
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

// writeSidecar must stay atomic and durable. A torn kosync.json fails to
// parse, which loses the progress and fails every later PUT for the book.
func writeSidecar(sidecars *os.Root, data SidecarData) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return library.WriteSidecarFile(sidecars, "kosync.json", raw)
}
