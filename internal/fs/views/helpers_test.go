package views

import (
	"errors"
	"slices"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/library"

	fsbook "github.com/ramblingenzyme/ebookfs/internal/fs/book"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
)

var (
	makeBook  = testutil.MakeMutableBook
	newTestFS = testutil.NewTestFS
)

func wrapBook(b *book.Book) *library.Book {
	return book.NewImmutableBook(b)
}

func makeBookWithSeries(id int64, title, author string, seriesName, seriesIndex string) *library.Book {
	b := makeBook(id, title, author)
	b.Series = &library.Series{Name: seriesName, Index: seriesIndex}
	return wrapBook(b)
}

// newTestRegistry builds a registry over a fresh in-memory FS with no backing
// library, for driving views through their Add/Remove notifications.
func newTestRegistry(t *testing.T) *registry.BookRegistry {
	t.Helper()
	return registry.NewBookRegistry(newTestFS(t), nil)
}

// statsReader is a StatsReader. With no hook it reports an empty library, which
// is what a real Stats call over an empty index returns.
type statsReader struct {
	StatsFn func() (*library.Stats, error)
}

func (s statsReader) Stats() (*library.Stats, error) {
	if s.StatsFn != nil {
		return s.StatsFn()
	}
	return &library.Stats{}, nil
}

var _ StatsReader = statsReader{}

// readerExporter is a ReaderExporter. Membership is the only thing these tests
// drive, so StatusList is the only hook; the four methods the view calls have
// the real naming behaviour behind them. Open and Size are what the view passes
// down to a ReaderFile, and no test here reads through one.
type readerExporter struct {
	StatusList []string
}

func (e readerExporter) Includes(b *library.Book) bool {
	return slices.Contains(e.StatusList, b.Status())
}

func (e readerExporter) Dirname(b *library.Book) string {
	return book.JoinAuthors(b.Authors(), " & ")
}

func (e readerExporter) Filename(b *library.Book) string { return b.Filename() }
func (e readerExporter) Warm(*library.Book)              {}

func (e readerExporter) Open(*library.Book) (library.EpubReader, error) {
	return nil, errors.New("readerExporter: no rendition in the views tests")
}

func (e readerExporter) Size(*library.Book) (int64, bool) { return 0, false }

var _ ReaderExporter = readerExporter{}

// contentReader stands in for the library a BookDir reads through. No test here
// reaches that path, so it only has to exist.
type contentReader struct{}

func (contentReader) Content(int64) (library.EpubReader, error) {
	return nil, errors.New("contentReader: no content in the views tests")
}

var _ fsbook.ContentReader = contentReader{}
