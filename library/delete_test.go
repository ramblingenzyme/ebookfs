package library

import (
	"errors"
	"os"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

func TestDeleteRemovesBook(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "To Delete"))
	id := book.ID()

	if err := lib.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Book should no longer be queryable.
	results, err := lib.Search(model.Query{IDs: []int64{id}})
	if err != nil {
		t.Fatalf("Query after delete: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Query returned %d results after delete, want 0", len(results))
	}
}

func TestDeleteRemovesOnDisk(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Delete On Disk"))
	id := book.ID()

	if err := lib.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// The epub file must no longer exist on disk.
	if _, err := os.Stat(book.EpubPath()); !os.IsNotExist(err) {
		t.Errorf("epub should be removed after delete, stat err = %v", err)
	}
}

func TestDeleteNonexistentBookErrors(t *testing.T) {
	lib := openTestLibrary(t)
	err := lib.Delete(9999)
	if err == nil {
		t.Fatal("expected error when deleting a non-existent book")
	}
	if !errors.Is(err, ErrBookNotFound) {
		t.Errorf("error = %v, want ErrBookNotFound", err)
	}
}
