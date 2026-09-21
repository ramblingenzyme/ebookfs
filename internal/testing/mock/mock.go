// Package mock holds the doubles for the library facade, one per interface a
// consumer declares and under that interface's name, so mock.Editor doubles
// registry.Editor. A wider double embeds the narrower one it contains.
//
// A nil hook fails loudly where a zero value would let a test pass without
// exercising the path it names, and otherwise returns the real empty answer.
//
// A composite literal cannot set a promoted field, so the hook a test stubs
// most often is a direct field. An embedded field shadows a method of the same
// name, so an embedded type is named for its interface (ContentReader) rather
// than for its method (Content).
//
// It imports library, which internal/testing/util cannot; that package's doc
// says why.
package mock

import (
	"bytes"
	"errors"
	"slices"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

type ContentReader struct {
	ContentFn func(int64) (library.EpubReader, error)
}

func (c ContentReader) Content(id int64) (library.EpubReader, error) {
	if c.ContentFn != nil {
		return c.ContentFn(id)
	}
	return nil, errors.New("mock: no ContentFn")
}

type Editor struct {
	ContentReader
	EditFn func(int64, library.Edits) (*library.Book, error)
}

func (e Editor) Edit(id int64, edits library.Edits) (*library.Book, error) {
	if e.EditFn != nil {
		return e.EditFn(id, edits)
	}
	return nil, errors.New("mock: no EditFn")
}

type SearchDeleter struct {
	SearchFn func(library.Query) ([]*library.Book, error)
	DeleteFn func(int64) error
}

func (l SearchDeleter) Search(q library.Query) ([]*library.Book, error) {
	if l.SearchFn != nil {
		return l.SearchFn(q)
	}
	return nil, nil
}

func (l SearchDeleter) Delete(id int64) error {
	if l.DeleteFn != nil {
		return l.DeleteFn(id)
	}
	return nil
}

type StatsReader struct {
	StatsFn func() (*library.Stats, error)
}

func (s StatsReader) Stats() (*library.Stats, error) {
	if s.StatsFn != nil {
		return s.StatsFn()
	}
	return &library.Stats{}, nil
}

type Renderer struct {
	OpenFn func(*library.Book) (library.EpubReader, error)
	SizeFn func(*library.Book) (int64, bool)
}

func (r Renderer) Open(b *library.Book) (library.EpubReader, error) {
	if r.OpenFn != nil {
		return r.OpenFn(b)
	}
	return nil, errors.New("mock: no OpenFn")
}

func (r Renderer) Size(b *library.Book) (int64, bool) {
	if r.SizeFn != nil {
		return r.SizeFn(b)
	}
	return 0, false
}

type Exporter struct {
	Renderer
	StatusList []string
}

func (e Exporter) Includes(b *library.Book) bool {
	return slices.Contains(e.StatusList, b.Status())
}

func (e Exporter) Dirname(b *library.Book) string {
	return book.JoinAuthors(b.Authors(), book.AuthorSep)
}

func (e Exporter) Filename(b *library.Book) string { return b.Filename() }
func (e Exporter) Warm(*library.Book)              {}

type IngestHandle struct {
	IngestFn func(string) (*library.Book, error)
}

func (h IngestHandle) WriteAt(p []byte, _ int64) (int, error) { return len(p), nil }

func (h IngestHandle) Ingest() (*library.Book, error) {
	if h.IngestFn == nil {
		return nil, nil
	}
	return h.IngestFn("")
}

type Ingester struct {
	CreateIngestFn func() (library.IngestHandle, error)
	IngestFn       func(string) (*library.Book, error)
}

func (i Ingester) CreateIngest() (library.IngestHandle, error) {
	if i.CreateIngestFn != nil {
		return i.CreateIngestFn()
	}
	return IngestHandle{IngestFn: i.IngestFn}, nil
}

type EpubReader struct {
	*bytes.Reader
	Closed  bool
	OPFFn   func() ([]byte, error)
	CoverFn func() ([]byte, error)
}

func (r *EpubReader) Close() error {
	r.Closed = true
	return nil
}

func (r *EpubReader) OPF() ([]byte, error) {
	if r.OPFFn != nil {
		return r.OPFFn()
	}
	return nil, nil
}

func (r *EpubReader) Cover() ([]byte, error) {
	if r.CoverFn != nil {
		return r.CoverFn()
	}
	return nil, nil
}

type Library struct {
	Editor
	SearchDeleter
	Ingester
	StatsReader
}
