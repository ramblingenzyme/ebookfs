package fs

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

func TestSetupServer(t *testing.T) {
	lib := libfake.Lib{
		QueryFn: func(_ model.Filter) ([]*model.Book, error) {
			b1 := makeBook(1, "Book One", "Alice")
			b1.Meta.Status = "unread"
			b2 := makeBook(2, "Book Two", "Bob")
			b2.Meta.Status = "read"
			return []*model.Book{b1, b2}, nil
		},
	}
	exp := libfake.Exporter{StatusList: []string{"unread"}}

	_, root, err := setupServer(lib, exp)
	if err != nil {
		t.Fatalf("setupServer: %v", err)
	}
	if root == nil {
		t.Fatal("setupServer returned nil root")
	}

	wantChildren := []string{"inbox", "books", "by-author", "by-id", "by-series", "reader"}
	for _, name := range wantChildren {
		if _, ok := root.Children()[name]; !ok {
			t.Errorf("root should have child %q", name)
		}
	}
}

func TestSetupServer_QueryError(t *testing.T) {
	lib := libfake.Lib{
		QueryFn: func(_ model.Filter) ([]*model.Book, error) {
			return nil, errTest
		},
	}
	_, _, err := setupServer(lib, libfake.Exporter{})
	if err == nil {
		t.Fatal("expected error from setupServer when Query fails")
	}
}

func TestSetupServer_BooksPopulated(t *testing.T) {
	lib := libfake.Lib{
		QueryFn: func(_ model.Filter) ([]*model.Book, error) {
			b := makeBook(1, "Present", "Alice")
			b.Meta.Status = "unread"
			return []*model.Book{b}, nil
		},
	}
	_, root, err := setupServer(lib, libfake.Exporter{})
	if err != nil {
		t.Fatalf("setupServer: %v", err)
	}

	allBooks := root.Children()["books"].(*bookListDir)
	if _, ok := allBooks.Children()["Present"]; !ok {
		t.Errorf("books view should contain 'Present' after setup")
	}
}
