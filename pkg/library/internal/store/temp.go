package store

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	epubfile "github.com/ramblingenzyme/ebookfs/pkg/epub"
)

const tempPrefix = ".ebookfs-"

// tempPattern is the os.CreateTemp pattern for a write that replaces name.
func tempPattern(name string) string {
	return tempPrefix + "*." + strings.TrimPrefix(name, ".") + ".tmp"
}

// staleTempPatterns match only the temp files this package and pkg/epub's Save
// write. The tree can hold another tool's write in progress, so a bare *.tmp
// would delete it.
var staleTempPatterns = []string{tempPrefix + "*.tmp", epubfile.SaveTempPattern}

// RemoveStaleTemps removes the temp files that crashed writes leave in the
// tree. Run it only while no write is in progress. Only a failure on the root
// itself is returned.
func (s *Store) RemoveStaleTemps() error {
	return filepath.Walk(s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if path == s.root {
				return err
			}
			// Returning nil skips this path and keeps walking the other books.
			slog.Warn("skipping unreadable path in stale temp sweep", "path", path, "error", err)
			return nil
		}
		if !info.Mode().IsRegular() || !isStaleTemp(info.Name()) {
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

func isStaleTemp(name string) bool {
	for _, p := range staleTempPatterns {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}
