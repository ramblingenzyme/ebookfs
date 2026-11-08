package library

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// cleanStaleTmpFiles walks the library root and removes the temp files that
// crashed epub and meta.toml writes leave beside a book. Without cleanup, they
// accumulate forever. Only a failure on root itself is returned.
func cleanStaleTmpFiles(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			// Returning nil skips this path and keeps walking the other books.
			slog.Warn("skipping unreadable path in stale temp sweep", "path", path, "error", err)
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if strings.HasPrefix(info.Name(), ".ebookfs-") && strings.HasSuffix(info.Name(), ".tmp") {
			if err := os.Remove(path); err != nil {
				slog.Warn("removing stale temp failed", "path", path, "error", err)
			} else {
				slog.Info("removed stale temp", "path", path)
			}
		}
		return nil
	})
}
