// Package atomicfs provides atomic file operations with fsync guarantees.
// All operations ensure that after they return successfully, the data is
// durable on disk and will survive a crash.
package atomicfs

import (
	"fmt"
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

// Rename atomically renames src to dst and fsyncs the parent directory.
// This ensures that after Rename returns successfully, the rename is durable
// and will survive a crash.
func Rename(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		return err
	}

	// fsync parent directory to ensure the rename is durable
	if err := syncDir(filepath.Dir(dst)); err != nil {
		return err
	}

	return nil
}

// syncDir opens a directory and syncs it to ensure directory entries are durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
