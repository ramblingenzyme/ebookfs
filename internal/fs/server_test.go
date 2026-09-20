package fs

import (
	"context"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/libtest"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/pkg/library"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/fstest"
)

func TestSetupServer(t *testing.T) {
	lib := libtest.Library{SearchDeleter: libtest.SearchDeleter{
		SearchFn: func(_ library.Query) ([]*library.Book, error) {
			b1 := makeBook(1, "Book One", "Alice")
			b1.Meta.Status = "unread"
			b2 := makeBook(2, "Book Two", "Bob")
			b2.Meta.Status = "read"
			return []*library.Book{testutil.WrapBook(b1), testutil.WrapBook(b2)}, nil
		},
	}}
	exp := libtest.Exporter{StatusList: []string{"unread"}}

	srv, err := SetupServer(lib, exp, 30*time.Minute, 100)
	if err != nil {
		t.Fatalf("SetupServer: %v", err)
	}
	if srv.root == nil {
		t.Fatal("SetupServer returned nil root")
	}
	srv.Shutdown(context.Background())

	wantChildren := []string{"inbox", "books", "by-author", "by-id", "by-series", "reader", "recent", "stats", "search"}
	for _, name := range wantChildren {
		fstest.HasChild(t, srv.root, name)
	}
}

func TestSetupServer_QueryError(t *testing.T) {
	lib := libtest.Library{SearchDeleter: libtest.SearchDeleter{
		SearchFn: func(_ library.Query) ([]*library.Book, error) {
			return nil, errTest
		},
	}}
	_, err := SetupServer(lib, libtest.Exporter{}, 30*time.Minute, 100)
	if err == nil {
		t.Fatal("expected error from SetupServer when Query fails")
	}
}

func TestSetupServer_BooksPopulated(t *testing.T) {
	lib := libtest.Library{SearchDeleter: libtest.SearchDeleter{
		SearchFn: func(_ library.Query) ([]*library.Book, error) {
			b := makeBook(1, "Present", "Alice")
			b.Meta.Status = "unread"
			return []*library.Book{testutil.WrapBook(b)}, nil
		},
	}}
	srv, err := SetupServer(lib, libtest.Exporter{}, 30*time.Minute, 100)
	if err != nil {
		t.Fatalf("SetupServer: %v", err)
	}
	srv.Shutdown(context.Background())

	fstest.HasChild(t, fstest.ChildAs[fs.Dir](t, srv.root, "books"), "Present")
}
