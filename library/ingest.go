package library

import (
	"io"
	"os"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// IngestHandle is a writable handle for staging an epub upload, returned by
// Library.CreateIngest. The frontend writes upload bytes via WriteAt, then
// calls Ingest to finalize: the file is closed, the epub is parsed and laid
// down in the store, and the temp file is cleaned up.
type IngestHandle interface {
	io.WriterAt // WriteAt(p []byte, off int64) (int, error)
	Ingest() (*model.Book, error)
}

// ingestHandle is the concrete file-backed implementation of IngestHandle.
type ingestHandle struct {
	file     *os.File
	path     string
	ingestFn func(string) (*model.Book, error)
}

func (h *ingestHandle) WriteAt(p []byte, off int64) (int, error) { return h.file.WriteAt(p, off) }

func (h *ingestHandle) Ingest() (*model.Book, error) {
	h.file.Close()
	if h.ingestFn == nil {
		return nil, nil
	}
	b, err := h.ingestFn(h.path)
	os.Remove(h.path)
	return b, err
}
