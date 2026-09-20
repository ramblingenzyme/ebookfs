// Package mock holds the doubles for the library facade, one per interface a
// consumer declares. A wider double embeds the narrower one it contains, so
// fs/book gets Renderer and its two methods rather than Exporter and its six.
// That narrowness is the point: the shared fake this replaces lacked it, which
// is why its doubles were once pulled back into each package.
//
// A nil hook fails loudly where a zero value would let a test pass without
// exercising the path it names, and otherwise returns the real empty answer: an
// empty library, a cold rendition.
//
// Two Go rules decide what sits where. A composite literal cannot set a promoted
// field, so the hook a test stubs most often is a direct field. An embedded
// field shadows a method of the same name, so an embedded type is named for its
// interface (ContentReader) rather than for its method (Content).
//
// It imports library, which is why it is separate from internal/testutil:
// library's own internal packages have white-box tests that import testutil.
package mock

import (
	"bytes"
	"errors"
	"slices"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// ContentReader is the read half of the backend: fs/book's ContentReader, and the
// half of registry's Editor that serves a book's own bytes.
type ContentReader struct {
	ContentFn func(int64) (library.EpubReader, error)
}

func (c ContentReader) Content(id int64) (library.EpubReader, error) {
	if c.ContentFn != nil {
		return c.ContentFn(id)
	}
	return nil, errors.New("mock: no ContentFn")
}

// Editor is registry's Editor, which fs/ctl writes through. EditFn is direct
// because stubbing the edit is the common case; a test that also serves
// content sets the embedded ContentReader.
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

// SearchDeleter is the half of the backend fs/ctl reads and deletes through.
// No SearchFn means an empty library, which is a real answer. No DeleteFn
// means the delete succeeded, since a command's own failure paths stub it.
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

// StatsReader is views' StatsReader. With no hook it reports an empty library, which
// is what a Stats call over an empty index returns.
type StatsReader struct {
	StatsFn func() (*library.Stats, error)
}

func (s StatsReader) Stats() (*library.Stats, error) {
	if s.StatsFn != nil {
		return s.StatsFn()
	}
	return &library.Stats{}, nil
}

// Renderer is fs/book's Renderer: the two exporter methods a ReaderFile uses.
// Open errors without a hook; Size reports cold, which is what the real
// exporter answers before a rendition is cached.
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

// Exporter is views' ReaderExporter: Renderer plus the four methods that
// belong to the view rather than the file. Membership runs off StatusList,
// which is the only part these tests drive, so it is the direct field; the
// naming methods have the real behaviour behind them.
type Exporter struct {
	Renderer
	StatusList []string
}

func (e Exporter) Includes(b *library.Book) bool {
	return slices.Contains(e.StatusList, b.Status())
}

func (e Exporter) Dirname(b *library.Book) string {
	return book.JoinAuthors(b.Authors(), " & ")
}

func (e Exporter) Filename(b *library.Book) string { return b.Filename() }
func (e Exporter) Warm(*library.Book)              {}

// IngestHandle is a library.IngestHandle. WriteAt accepts and discards bytes,
// since no test reads them back; Ingest with no hook yields a nil book.
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

// Ingester is fs/inbox's Ingester. It carries IngestFn rather than embedding a
// handle so an upload test, which is every test but one, stubs the ingest in a
// flat literal. CreateIngestFn is for the one test that fails the create.
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

// EpubReader is a library.EpubReader over a fixed buffer, with the OPF and
// cover extraction results injected. Closed records the clunk, which is what
// the vfile tests assert about a released fid.
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

// Library is the union fs.SetupServer takes. Every hook name is distinct
// across the four, so each promotes to depth one and a test reaches e.g.
// lib.SearchFn without naming which half it came from.
type Library struct {
	Editor
	SearchDeleter
	Ingester
	StatsReader
}
