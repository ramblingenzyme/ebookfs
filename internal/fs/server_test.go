package fs

import (
	"testing"
)

// A closed library fails every query, which reaches New as the startup search
// that loads the tree.
func TestNew_QueryError(t *testing.T) {
	lib, _ := openLibrary(t)
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := New(lib, Config{}); err == nil {
		t.Fatal("New succeeded over a library that cannot be searched")
	}
}
