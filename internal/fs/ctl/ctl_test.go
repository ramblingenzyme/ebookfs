package ctl

import (
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
)

// The file through the 9P Write/Close cycle: the command runs and its outcome
// is recorded in the log, while reads return only a usage hint (ctl does not
// echo command results).
func TestCtlFileWriteExecutes(t *testing.T) {
	called := false
	search := mock.SearchDeleter{DeleteFn: func(int64) error { called = true; return nil }}
	cf, _ := newTestCtl(t, search, mock.Editor{})

	// Reading returns a usage hint, not command output.
	fid := fstest.Fid(t, cf, 1)
	if got := fid.Read(0, 4096); strings.Contains(got, "book 7 deleted") {
		t.Fatalf("read should not echo command results, got %q", got)
	}

	// Writing a command and closing the fid executes it...
	fid.Write(0, "delete 7")
	fid.Close()
	if !called {
		t.Fatal("delete command was not executed on close")
	}

	// ...and the outcome is recorded in the command log.
	entries := cf.cmdLog.Entries()
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(entries))
	}
	if entries[0].Command != "delete 7" {
		t.Errorf("logged command = %q, want delete 7", entries[0].Command)
	}
	if !strings.Contains(entries[0].Result, "book 7 deleted") {
		t.Errorf("logged result = %q, want the delete result", entries[0].Result)
	}
}
