package library

import (
	"testing"
)

func TestClose(t *testing.T) {
	lib := openTestLibrary(t)
	// First close must succeed and not panic.
	lib.Close()
}

func TestCloseMultiple(t *testing.T) {
	lib := openTestLibrary(t)
	lib.Close()
	// Second close must not panic or deadlock.
	lib.Close()
}

func TestOpenCloseRoundTrip(t *testing.T) {
	// Open and close without any operations in between.
	lib := openTestLibrary(t)
	lib.Close()
}
