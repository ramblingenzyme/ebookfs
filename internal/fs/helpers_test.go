package fs

// Server.Start is untested: it calls go9p.Serve, which blocks. SetupServer
// covers the wiring instead, and the e2e build tag covers a served tree.

import (
	"errors"
	"slices"

	bookmodel "github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/fs/book"
	"github.com/ramblingenzyme/ebookfs/internal/fs/views"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/library"
)

// The book and FS helpers live in internal/testutil. These aliases let the
// composition tests call them unqualified.
var (
	makeBook  = testutil.MakeMutableBook
	newTestFS = testutil.NewTestFS
	errTest   = testutil.ErrTest
)

// The doubles below are one per interface the tree declares, so a scenario test
// names the half of the backend it exercises. Only server_test needs all of
// them at once, because SetupServer takes the Library union.

// editor is a registry.Editor. EditFn errors when unset so a test that reaches
// the edit path without stubbing it fails loudly. Nothing here reads a book's
// content through the tree, so Content only has to exist.
type editor struct {
	EditFn func(int64, library.Edits) (*library.Book, error)
}

func (e editor) Edit(id int64, edits library.Edits) (*library.Book, error) {
	if e.EditFn != nil {
		return e.EditFn(id, edits)
	}
	return nil, errors.New("editor: no EditFn")
}

func (e editor) Content(int64) (library.EpubReader, error) {
	return nil, errors.New("editor: no content in the composition tests")
}

// searchDeleter is a ctl.SearchDeleter. No SearchFn means an empty library.
// Delete always succeeds: the delete command's own failures are covered in
// fs/ctl, and what these tests watch is the tree.
type searchDeleter struct {
	SearchFn func(library.Query) ([]*library.Book, error)
}

func (l searchDeleter) Search(q library.Query) ([]*library.Book, error) {
	if l.SearchFn != nil {
		return l.SearchFn(q)
	}
	return nil, nil
}

func (l searchDeleter) Delete(int64) error { return nil }

// ingester is an inbox.Ingester, handing back a handle wired to IngestFn.
type ingester struct {
	IngestFn func(string) (*library.Book, error)
}

func (i ingester) CreateIngest() (library.IngestHandle, error) {
	return ingestHandle{IngestFn: i.IngestFn}, nil
}

// ingestHandle is a library.IngestHandle. WriteAt accepts and discards bytes,
// since no test reads them back.
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

// statsReader is a views.StatsReader. Only the Library union needs it, and no
// test here reads stats/, so it reports an empty library.
type statsReader struct{}

func (statsReader) Stats() (*library.Stats, error) { return &library.Stats{}, nil }

// exporter is a views.ReaderExporter. Membership runs off StatusList, which is
// all these tests drive; the two methods it passes down to a ReaderFile behave
// like a cold rendition.
type exporter struct {
	StatusList []string
}

func (e exporter) Includes(b *library.Book) bool {
	return slices.Contains(e.StatusList, b.Status())
}

func (e exporter) Dirname(b *library.Book) string {
	return bookmodel.JoinAuthors(b.Authors(), " & ")
}

func (e exporter) Filename(b *library.Book) string { return b.Filename() }
func (e exporter) Warm(*library.Book)              {}

func (e exporter) Open(*library.Book) (library.EpubReader, error) {
	return nil, errors.New("exporter: no rendition in the composition tests")
}

func (e exporter) Size(*library.Book) (int64, bool) { return 0, false }

var (
	_ book.Renderer        = exporter{}
	_ views.ReaderExporter = exporter{}
)

// fakeLibrary is the Library union, needed only by SetupServer.
type fakeLibrary struct {
	editor
	searchDeleter
	ingester
	statsReader
}

var _ Library = fakeLibrary{}
