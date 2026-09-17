package inbox

import (
	"github.com/ramblingenzyme/ebookfs/library"
)

// ingester is an Ingester. With no CreateIngestFn it hands back a handle wired
// to IngestFn, which is the path every upload test takes; CreateIngestFn exists
// for the one test that makes the create itself fail.
type ingester struct {
	CreateIngestFn func() (library.IngestHandle, error)
	IngestFn       func(string) (*library.Book, error)
}

func (i ingester) CreateIngest() (library.IngestHandle, error) {
	if i.CreateIngestFn != nil {
		return i.CreateIngestFn()
	}
	return ingestHandle{IngestFn: i.IngestFn}, nil
}

var _ Ingester = ingester{}

// ingestHandle is a library.IngestHandle. WriteAt accepts and discards bytes,
// since no test reads them back; Ingest with no hook yields a nil book.
type ingestHandle struct {
	IngestFn func(string) (*library.Book, error)
}

func (h ingestHandle) WriteAt(p []byte, _ int64) (int, error) { return len(p), nil }

func (h ingestHandle) Ingest() (*library.Book, error) {
	if h.IngestFn == nil {
		return nil, nil
	}
	return h.IngestFn("")
}

var _ library.IngestHandle = ingestHandle{}
