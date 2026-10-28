package atomicfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	err := Write(dir, "test-*.tmp", func(f *os.File) error {
		_, err := f.WriteString("hello world")
		return err
	}, path)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Verify file exists and has correct content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("got %q, want %q", string(data), "hello world")
	}

	// Verify no temp files remain
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temp file %q remains after Write", e.Name())
		}
	}
}

func TestWriteError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	expectedErr := errors.New("write failed")

	err := Write(dir, "test-*.tmp", func(f *os.File) error {
		return expectedErr
	}, path)
	if err == nil {
		t.Fatal("Write should have failed")
	}

	// Verify target file does not exist
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("target file should not exist after failed Write")
	}

	// Verify no temp files remain
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temp file %q remains after failed Write", e.Name())
		}
	}
}

func TestRenameSuccess(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")

	// Create source file
	if err := os.WriteFile(src, []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	err := Rename(src, dst)
	if err != nil {
		t.Fatalf("Rename failed: %v", err)
	}

	// Verify source no longer exists
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source file should not exist after Rename")
	}

	// Verify destination exists with correct content
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != "test" {
		t.Errorf("got %q, want %q", string(data), "test")
	}
}

func TestRenameSourceNotExist(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "nonexistent.txt")
	dst := filepath.Join(dir, "dst.txt")

	err := Rename(src, dst)
	if err == nil {
		t.Fatal("Rename should have failed for nonexistent source")
	}
}
