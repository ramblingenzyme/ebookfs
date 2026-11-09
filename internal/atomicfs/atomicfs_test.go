package atomicfs

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// A failed directory fsync comes after the rename, so returning it would tell
// callers the file hadn't moved. Each parent gets a turn as the failing one,
// which also shows that both are synced.
func TestRenameLogsDirSyncFailure(t *testing.T) {
	for _, unopenable := range []string{"from", "to"} {
		t.Run(unopenable, func(t *testing.T) {
			logs := captureLogs(t)
			base := t.TempDir()
			src := filepath.Join(base, "from", "f")
			dst := filepath.Join(base, "to", "f")
			for _, dir := range []string{"from", "to"} {
				if err := os.Mkdir(filepath.Join(base, dir), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(src, []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
			failing := filepath.Join(base, unopenable)
			makeUnopenable(t, failing)

			if err := Rename(src, dst); err != nil {
				t.Fatalf("Rename: %v", err)
			}
			if _, err := os.Stat(dst); err != nil {
				t.Errorf("dst after Rename: %v", err)
			}
			if !strings.Contains(logs.String(), failing) {
				t.Errorf("no warning naming %s, logs: %q", failing, logs)
			}
		})
	}
}

// The parent of each new directory is synced, and an existing tree isn't.
func TestMkdirAllSyncsParentsOfNewDirs(t *testing.T) {
	logs := captureLogs(t)
	base := filepath.Join(t.TempDir(), "base")
	if err := os.Mkdir(base, 0755); err != nil {
		t.Fatal(err)
	}
	makeUnopenable(t, base)
	path := filepath.Join(base, "a", "b")

	if err := MkdirAll(path, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		t.Fatalf("Stat(%s) = %v, %v", path, fi, err)
	}
	if !strings.Contains(logs.String(), base) {
		t.Errorf("no warning naming %s, logs: %q", base, logs)
	}

	logs.Reset()
	if err := MkdirAll(path, 0755); err != nil {
		t.Fatalf("MkdirAll on existing path: %v", err)
	}
	if logs.Len() != 0 {
		t.Errorf("existing path logged %q", logs)
	}
}

func TestMkdirAllUnderFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := MkdirAll(filepath.Join(file, "a", "b"), 0755); err == nil {
		t.Fatal("MkdirAll under a file should fail")
	}
}

func TestWriteRootReplaces(t *testing.T) {
	logs := captureLogs(t)
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	for _, want := range []string{"first", "second"} {
		if err := WriteRoot(root, "f.json", []byte(want)); err != nil {
			t.Fatalf("WriteRoot(%q): %v", want, err)
		}
		got, err := root.ReadFile("f.json")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("entries = %v, want only f.json", entries)
	}
	if logs.Len() != 0 {
		t.Errorf("logged %q", logs)
	}
}

// slog is process-global, so tests using this can't run in parallel.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// makeUnopenable leaves dir writable and traversable, so a rename into it works
// but opening it for fsync fails. Root ignores the mode.
func makeUnopenable(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	if err := os.Chmod(dir, 0300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0755) })
}

func TestRemoveAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book")
	if err := os.MkdirAll(filepath.Join(path, ".sidecar"), 0755); err != nil {
		t.Fatal(err)
	}
	trash := filepath.Join(dir, ".trash")

	if err := RemoveAll(path, trash); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("path after RemoveAll: err = %v, want it gone", err)
	}
	if entries, _ := os.ReadDir(trash); len(entries) != 0 {
		t.Errorf("trash holds %d entries, want none", len(entries))
	}
}

func TestRemoveAllMissingPath(t *testing.T) {
	dir := t.TempDir()
	if err := RemoveAll(filepath.Join(dir, "gone"), filepath.Join(dir, ".trash")); err != nil {
		t.Errorf("RemoveAll of a missing path: %v", err)
	}
}

// Once path is in the trash the removal has happened, so a failure to delete
// the trashed copy is reported as success.
func TestRemoveAllSucceedsWhenTrashRemovalFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "book")
	stuck := filepath.Join(path, "stuck")
	if err := os.MkdirAll(stuck, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stuck, "f"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stuck, 0555); err != nil {
		t.Skip("cannot chmod:", err)
	}
	trash := filepath.Join(dir, ".trash")
	t.Cleanup(func() {
		filepath.WalkDir(trash, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				os.Chmod(p, 0755)
			}
			return nil
		})
	})

	if err := RemoveAll(path, trash); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("path after RemoveAll: err = %v, want it gone", err)
	}
	if entries, _ := os.ReadDir(trash); len(entries) != 1 {
		t.Errorf("trash holds %d entries, want the one copy it could not remove", len(entries))
	}
}
