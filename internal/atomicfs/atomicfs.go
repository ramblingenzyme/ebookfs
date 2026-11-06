// Package atomicfs provides atomic file operations with fsync guarantees. A nil
// return means the data reached disk and the rename landed. The rename survives
// a crash unless syncDir logged a failure.
package atomicfs

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// Write atomically writes to path by creating a temp file in dir, calling writeFunc,
// fsyncing the file, closing it, renaming to path, and fsyncing the parent directory.
// The writeFunc receives an open file handle and is responsible for writing data.
// If writeFunc returns an error, the temp file is removed and path is left unchanged.
func Write(dir, pattern string, writeFunc func(*os.File) error, path string) error {
	tmp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // Clean up on any error path

	if err := writeFunc(tmp); err != nil {
		tmp.Close()
		return fmt.Errorf("write func: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp: %w", err)
	}

	if err := Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming: %w", err)
	}

	return nil
}

// WriteRoot replaces name inside root with data, like Write. Its temp name is
// fixed, so the caller serializes writers of the same name.
func WriteRoot(root *os.Root, name string, data []byte) error {
	tmpName := "." + name + ".tmp"
	defer root.Remove(tmpName)

	tmp, err := root.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}

	if err := root.Rename(tmpName, name); err != nil {
		return err
	}
	syncDir(root.Open("."))
	return nil
}

// Rename atomically renames src to dst and fsyncs both parent directories. An
// error means nothing moved. The destination syncs first, so a crash between
// the two can leave both names but cannot lose the file.
func Rename(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		return err
	}

	srcDir, dstDir := filepath.Dir(src), filepath.Dir(dst)
	syncDir(os.Open(dstDir))
	if srcDir != dstDir {
		syncDir(os.Open(srcDir))
	}
	return nil
}

// Mkdir is os.Mkdir plus an fsync of the parent.
func Mkdir(path string, perm os.FileMode) error {
	if err := os.Mkdir(path, perm); err != nil {
		return err
	}
	syncDir(os.Open(filepath.Dir(path)))
	return nil
}

// MkdirAll is os.MkdirAll plus an fsync of each new directory's parent. Rename
// syncs only the directory it lands in, so a crash could otherwise drop that
// directory from its own parent.
func MkdirAll(path string, perm os.FileMode) error {
	var parents []string
	for dir := path; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			break
		}
		parents = append(parents, filepath.Dir(dir))
	}

	if err := os.MkdirAll(path, perm); err != nil {
		return err
	}
	for _, parent := range parents {
		syncDir(os.Open(parent))
	}
	return nil
}

// syncDir takes the results of opening a directory. It logs a failure and
// returns nothing, since the rename has already happened and callers read any
// error as nothing moved. Ingest would delete the book it just filed.
func syncDir(d *os.File, err error) {
	if err == nil {
		defer d.Close()
		err = d.Sync()
	}
	if err != nil {
		slog.Warn("atomicfs: directory fsync failed, a crash could lose its recent changes", "error", err)
	}
}
