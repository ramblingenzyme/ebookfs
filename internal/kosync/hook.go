package kosync

import (
	"encoding/json"
	"log/slog"
	"os"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// Hook implements library.Hook for kosync document ID computation.
type Hook struct {
	library.HookBase
	mapping *MappingFile
}

// NewHook creates a kosync hook that manages document IDs and mapping.
func NewHook(mapping *MappingFile) *Hook {
	return &Hook{mapping: mapping}
}

// SidecarData is the structure stored in .sidecar/kosync.json
type SidecarData struct {
	DocumentIDs []string `json:"document_ids"`
	Progress    Progress `json:"progress"`
}

// OnIngested computes the document ID and initializes the sidecar.
func (h *Hook) OnIngested(book *library.Book, sidecars *os.Root, openEpub func() (library.EpubReader, error)) {
	docID, err := h.computeDocumentID(book, openEpub)
	if err != nil {
		slog.Error("kosync: failed to compute document ID", "book_id", book.ID(), "error", err)
		return
	}

	// Initialize sidecar with document ID and empty progress
	data := SidecarData{
		DocumentIDs: []string{docID},
		Progress:    Progress{},
	}

	if err := writeSidecar(sidecars, data); err != nil {
		slog.Error("kosync: failed to write sidecar", "book_id", book.ID(), "error", err)
		return
	}

	h.setMapping(docID, book.ID())

	slog.Info("kosync: computed document ID", "book_id", book.ID(), "document_id", docID)
}

// OnEdited recomputes the document ID and updates the sidecar.
// Historical document IDs are preserved to maintain progress continuity.
func (h *Hook) OnEdited(book *library.Book, sidecars *os.Root, openEpub func() (library.EpubReader, error)) {
	newDocID, err := h.computeDocumentID(book, openEpub)
	if err != nil {
		slog.Error("kosync: failed to compute document ID", "book_id", book.ID(), "error", err)
		return
	}

	// Read existing sidecar
	data, err := readSidecar(sidecars)
	if err != nil {
		slog.Error("kosync: failed to read sidecar", "book_id", book.ID(), "error", err)
		return
	}

	// Check if document ID changed
	if len(data.DocumentIDs) > 0 && data.DocumentIDs[0] == newDocID {
		// No change
		return
	}

	// Add new document ID at the front, preserve historical IDs
	data.DocumentIDs = append([]string{newDocID}, data.DocumentIDs...)

	// Write updated sidecar
	if err := writeSidecar(sidecars, *data); err != nil {
		slog.Error("kosync: failed to write sidecar", "book_id", book.ID(), "error", err)
		return
	}

	// Add new document ID to mapping, keeping historical IDs for backward compatibility
	h.setMapping(newDocID, book.ID())

	slog.Info("kosync: updated document ID", "book_id", book.ID(), "document_id", newDocID)
}

// OnDeleting removes document IDs from the mapping before deletion.
func (h *Hook) OnDeleting(book *library.Book, sidecars *os.Root, openEpub func() (library.EpubReader, error)) {
	// Read sidecar to get all document IDs
	data, err := readSidecar(sidecars)
	if err != nil {
		slog.Error("kosync: failed to read sidecar for deletion", "book_id", book.ID(), "error", err)
		return
	}

	// Remove all document IDs from mapping
	for _, docID := range data.DocumentIDs {
		h.mapping.Delete(docID)
	}

	slog.Info("kosync: removed document IDs from mapping", "book_id", book.ID(), "count", len(data.DocumentIDs))
}

// ponytail: only the epub is hashed. With reader.convert the device holds the
// kepub, whose hash maps to nothing, so its progress is dropped. Map the
// kepub's hash too, refreshed on each reconversion, if convert and kosync are
// used together.
func (h *Hook) computeDocumentID(book *library.Book, openEpub func() (library.EpubReader, error)) (string, error) {
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

// setMapping sets a document ID mapping.
func (h *Hook) setMapping(docID string, bookID int64) {
	h.mapping.Set(docID, bookID)
}
