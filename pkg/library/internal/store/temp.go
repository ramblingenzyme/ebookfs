package store

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/formats"
)

const tempPrefix = ".ebookfs-"

// tempPattern is the os.CreateTemp pattern for a write that replaces name.
func tempPattern(name string) string {
	return tempPrefix + "*." + strings.TrimPrefix(name, ".") + ".tmp"
}

// RemoveStaleTemps removes the temp files that crashed writes leave in the
// tree. Run it only while no write is in progress. Only a failure on the root
// itself is returned.
func (s *Store) RemoveStaleTemps(reg *formats.Registry) error {
	return filepath.Walk(s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if path == s.root {
				return err
			}
			// Returning nil skips this path and keeps walking the other books.
			slog.Warn("skipping unreadable path in stale temp sweep", "path", path, "error", err)

			return nil
		}

		if !info.Mode().IsRegular() || !isStaleTemp(info.Name(), reg) {
			return nil
		}

		if err := os.Remove(path); err != nil {
			slog.Warn("removing stale temp failed", "path", path, "error", err)
		} else {
			slog.Info("removed stale temp", "path", path)
		}

		return nil
	})
}

func isStaleTemp(name string, reg *formats.Registry) bool {
	for _, p := range append([]string{tempPrefix + "*.tmp"}, reg.AllTempPatterns()...) {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}

	return false
}
