package library

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// cleanStaleTmpFiles walks the library root and removes any .ebookfs-*.epub.tmp
// files left behind by crashed epub saves. These files are created in the same
// directory as the book during atomic writes, and a crash mid-write leaves them
// behind. Without cleanup, they accumulate forever.
func cleanStaleTmpFiles(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		// Match the pattern created by os.CreateTemp(dir, ".ebookfs-*.epub.tmp")
		if strings.HasPrefix(info.Name(), ".ebookfs-") && strings.HasSuffix(info.Name(), ".epub.tmp") {
			if err := os.Remove(path); err != nil {
				slog.Warn("removing stale epub temp failed", "path", path, "error", err)
			} else {
				slog.Info("removed stale epub temp", "path", path)
			}
		}
		return nil
	})
}
