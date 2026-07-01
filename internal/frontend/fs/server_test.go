package fs

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

func TestSetupServer(t *testing.T) {
	lib := fakeLib{
		listAllFn: func() ([]*model.Book, error) {
			b1 := makeBook(1, "Book One", "Alice")
			b1.Meta.Status = "unread"
			b2 := makeBook(2, "Book Two", "Bob")
			b2.Meta.Status = "read"
			return []*model.Book{b1, b2}, nil
		},
	}
	exp := testExporter{}
	cfg := ReaderConfig{Statuses: []string{"unread"}}

	_, root, err := setupServer(lib, exp, cfg, "/tmp")
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

func TestSetupServer_ListAllError(t *testing.T) {
	lib := fakeLib{
		listAllFn: func() ([]*model.Book, error) {
			return nil, errTest
		},
	}
	_, _, err := setupServer(lib, testExporter{}, ReaderConfig{}, "/tmp")
	if err == nil {
		t.Fatal("expected error from setupServer when ListAll fails")
	}
}

func TestSetupServer_BooksPopulated(t *testing.T) {
	lib := fakeLib{
		listAllFn: func() ([]*model.Book, error) {
			b := makeBook(1, "Present", "Alice")
			b.Meta.Status = "unread"
			return []*model.Book{b}, nil
		},
	}
	_, root, err := setupServer(lib, testExporter{}, ReaderConfig{}, "/tmp")
	if err != nil {
		t.Fatalf("setupServer: %v", err)
	}

	allBooks := root.Children()["books"].(*booksDir)
	if _, ok := allBooks.Children()["Present"]; !ok {
		t.Errorf("books view should contain 'Present' after setup")
	}
}
