package library

import (
	"os"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// IngestHandle is a writable handle returned by Library.CreateIngest.
// The frontend writes upload bytes via WriteAt, then calls Ingest to
// finalize: the file is closed, the epub is parsed and laid down in the
// store, and the temp file is cleaned up.
type IngestHandle struct {
	File     *os.File
	Path     string
	IngestFn func(string) (*model.Book, error)
}

func (h *IngestHandle) WriteAt(p []byte, off int64) (int, error) { return h.File.WriteAt(p, off) }

func (h *IngestHandle) Ingest() (*model.Book, error) {
	h.File.Close()
	if h.IngestFn == nil {
		return nil, nil
	}
	b, err := h.IngestFn(h.Path)
	os.Remove(h.Path)
	return b, err
}
