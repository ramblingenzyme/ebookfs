package fs

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// A closed library fails every query, which reaches New as the startup search
// that loads the tree.
func TestNew_QueryError(t *testing.T) {
	lib, _ := openLibrary(t)
	exp, err := lib.Exporter(library.ReaderConfig{})
	if err != nil {
		t.Fatalf("Exporter: %v", err)
	}
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := New(lib, exp, Config{}); err == nil {
		t.Fatal("New succeeded over a library that cannot be searched")
	}
}
