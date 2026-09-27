package library

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
)

func buildTestEpub(t *testing.T, title string, authors ...string) []byte {
	t.Helper()
	return util.BuildTestEpub(t, title, authors...)
}

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config(util.TestConfig(t))
}

func openTestLibrary(t *testing.T) *Library {
	t.Helper()
	return openLib(t, testConfig(t))
}

func openLib(t *testing.T, cfg Config, opts ...Option) *Library {
	t.Helper()
	lib, err := Open(cfg, opts...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { lib.Close() })
	return lib
}

func drifted(t *testing.T, lib *Library) bool {
	t.Helper()
	_, d := lib.storeDrifted()
	return d
}

// The store keeps the sidecar's filename private, so tests restate it here
// once.
func metaPathOf(book *Book, root string) string {
	return filepath.Join(root, book.Dir(), "meta.toml")
}

// store.Walk still lists the directory, but os.Stat follows the dangling
// symlink and fails. That is the only way to reach the rebuild's
// could-not-observe path from a test.
func breakEpub(t *testing.T, book *Book, root string) {
	t.Helper()
	absEpub := filepath.Join(root, book.EpubPath())
	if err := os.Remove(absEpub); err != nil {
		t.Fatalf("remove epub: %v", err)
	}
	dangling := filepath.Join(filepath.Dir(absEpub), "nowhere.epub")
	if err := os.Symlink(dangling, absEpub); err != nil {
		t.Fatalf("symlink: %v", err)
	}
}

// The id sequence lives in the index, so only a rebuild without one depends on
// the store's id reservations.
func dropIndex(t *testing.T, cfg Config) {
	t.Helper()
	// The WAL and shared-memory sidecars would otherwise resurrect the sequence.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(cfg.IndexPath + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove index%s: %v", suffix, err)
		}
	}
}

// A rebuild that forgets an unreadable book sees it as drift on every startup.
// Two restarts prove it: one records, one confirms. Both close explicitly,
// since the point is what a later process sees.
func assertSettlesClean(t *testing.T, cfg Config) {
	t.Helper()
	for i := 1; i <= 2; i++ {
		lib, err := Open(cfg)
		if err != nil {
			t.Fatalf("reopen %d: %v", i, err)
		}
		d := drifted(t, lib)
		if err := lib.Close(); err != nil {
			t.Fatalf("Close %d: %v", i, err)
		}
		if d {
			t.Fatalf("storeDrifted() = true on restart %d — the rebuild did not record what it found, "+
				"so one unreadable book forces a full reindex on every startup", i)
		}
	}
}

func ingestTestEpub(t *testing.T, lib *Library, data []byte) *Book {
	//goland:noinspection DuplicatedCode
	t.Helper()
	h, err := lib.CreateIngest()
	if err != nil {
		t.Fatalf("CreateIngest: %v", err)
	}
	if _, err := h.WriteAt(data, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	b, err := h.Ingest()
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return b
}
