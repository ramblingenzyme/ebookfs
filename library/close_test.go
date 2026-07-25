package library

import (
	"testing"
)

// TestClose asserts the returned error, which is the index's. openTestLibrary
// also registers a Close via t.Cleanup, so these tests double as coverage of
// closing an already-closed library.
func TestClose(t *testing.T) {
	lib := openTestLibrary(t)

	if err := lib.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestCloseMultiple pins that closing twice is safe and still reports success —
// the 9P server closes on shutdown and t.Cleanup closes again, so a second
// close returning an error would turn every test teardown into a failure.
func TestCloseMultiple(t *testing.T) {
	lib := openTestLibrary(t)

	if err := lib.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := lib.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}
