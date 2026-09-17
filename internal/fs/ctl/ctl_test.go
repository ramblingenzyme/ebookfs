package ctl

import (
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
)

// TestCtlFileWriteExecutes drives the file through the 9P Write/Close cycle and
// checks that the command runs and its outcome is recorded in the log, while
// reads return only a usage hint (ctl does not echo command results).
func TestCtlFileWriteExecutes(t *testing.T) {
	called := false
	lib := libfake.Lib{DeleteFn: func(int64) error { called = true; return nil }}
	reg, cmdLog := newTestCtl(t, lib)
	cf := NewCtlFile(reg.FS(), lib, reg, cmdLog)

	// Reading returns a usage hint, not command output.
	got, err := cf.Read(1, 0, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "book 7 deleted") {
		t.Fatalf("read should not echo command results, got %q", got)
	}

	// Writing a command and closing the fid executes it...
	if _, err := cf.Write(1, 0, []byte("delete 7")); err != nil {
		t.Fatal(err)
	}
	if err := cf.Close(1); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("delete command was not executed on close")
	}

	// ...and the outcome is recorded in the command log.
	entries := cmdLog.Entries()
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
