package library_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// A second Open on the same root, while the first is still open, gets
// ErrLibraryLocked. The error names the holder as "<hostname>:<pid>".
func TestSecondOpenGetsLocked(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg)

	_, err := library.Open(cfg)
	if err == nil {
		t.Fatal("second Open succeeded while first library is still open")
	}
	if !errors.Is(err, library.ErrLibraryLocked) {
		t.Fatalf("second Open err = %v, want ErrLibraryLocked", err)
	}

	// The holder string is "<hostname>:<pid>". Check the format.
	holder := strings.TrimPrefix(err.Error(), library.ErrLibraryLocked.Error()+": ")
	if !strings.Contains(holder, ":") {
		t.Errorf("holder = %q, want format \"<hostname>:<pid>\"", holder)
	}

	lib.Close()
}

// Close releases the lock, so a subsequent Open succeeds.
func TestLockReleasedOnClose(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg)
	lib.Close()

	lib2, err := library.Open(cfg)
	if err != nil {
		t.Fatalf("Open after Close: %v", err)
	}
	lib2.Close()
}

// The lock file contains "<hostname>:<pid>" after a successful Open.
func TestLockHolderWritten(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg)
	defer lib.Close()

	lockPath := filepath.Join(cfg.Root, ".lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, ":") {
		t.Errorf("lock file content = %q, want \"<hostname>:<pid>\"", content)
	}

	host, err := os.Hostname()
	if err != nil {
		t.Fatalf("os.Hostname: %v", err)
	}
	if !strings.HasPrefix(content, host+":") {
		t.Errorf("lock file content = %q, want prefix %q", content, host+":")
	}
}
