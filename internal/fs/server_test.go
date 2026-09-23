package fs

import (
	"context"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"

	"github.com/knusbaum/go9p/fs"
)

func TestNew(t *testing.T) {
	lib := mock.Library{SearchDeleter: mock.SearchDeleter{
		SearchFn: func(_ library.Query) ([]*library.Book, error) {
			b1 := makeBook(1, "Book One", "Alice")
			b1.Meta.Status = "unread"
			b2 := makeBook(2, "Book Two", "Bob")
			b2.Meta.Status = "read"
			return []*library.Book{util.WrapBook(b1), util.WrapBook(b2)}, nil
		},
	}}
	exp := mock.Exporter{StatusList: []string{"unread"}}

	srv, err := New(lib, exp, Config{SearchTTL: 30 * time.Minute, SearchMaxHandles: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.root == nil {
		t.Fatal("New returned nil root")
	}
	srv.Shutdown(context.Background())

	wantChildren := []string{"inbox", "books", "by-author", "by-id", "by-series", "reader", "recent", "stats", "search"}
	for _, name := range wantChildren {
		fstest.HasChild(t, srv.root, name)
	}
}

func TestNew_QueryError(t *testing.T) {
	lib := mock.Library{SearchDeleter: mock.SearchDeleter{
		SearchFn: func(_ library.Query) ([]*library.Book, error) {
			return nil, errTest
		},
	}}
	_, err := New(lib, mock.Exporter{}, Config{SearchTTL: 30 * time.Minute, SearchMaxHandles: 100})
	if err == nil {
		t.Fatal("expected error from New when Query fails")
	}
}

func TestNew_BooksPopulated(t *testing.T) {
	lib := mock.Library{SearchDeleter: mock.SearchDeleter{
		SearchFn: func(_ library.Query) ([]*library.Book, error) {
			b := makeBook(1, "Present", "Alice")
			b.Meta.Status = "unread"
			return []*library.Book{util.WrapBook(b)}, nil
		},
	}}
	srv, err := New(lib, mock.Exporter{}, Config{SearchTTL: 30 * time.Minute, SearchMaxHandles: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.Shutdown(context.Background())

	fstest.HasChild(t, fstest.ChildAs[fs.Dir](t, srv.root, "books"), "Present")
}
