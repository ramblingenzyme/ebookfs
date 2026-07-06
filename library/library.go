package library

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/internal/index"
	"github.com/ramblingenzyme/ebookfs/library/internal/store"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// EpubReader is a handle to a book's epub content. It hides where the bytes
// live (currently a file on disk) from the 9P layer, which needs random reads
// and a close.
type EpubReader = model.EpubReader

// Library defines the public API for filesystem and index operations on the
// book collection. The concrete implementation is unexported; construct via New.
//
// Concurrency contract: methods are safe for concurrent use. Returned
// *model.Book values are immutable snapshots — the library never mutates a
// Book after returning it. Reads take a snapshot ("read the version I'm
// looking at"); mutations (Edit, WriteCover, Delete) address a book by id and
// run as an atomic read-modify-write per book: the base state is fetched
// fresh under a per-book lock, so callers holding stale snapshots cannot
// revert other callers' changes.
type Library interface {
	Close() error
	CreateIngest() (*IngestHandle, error)
	Exporter(config.ReaderConfig) (Exporter, error)
	Query(model.Filter) ([]*model.Book, error)
	Reindex() error
	OpenEpub(b *model.Book) (EpubReader, error)
	ExtractCover(b *model.Book) ([]byte, error)
	ExtractOPF(b *model.Book) ([]byte, error)
	Edit(id int64, e model.Edits) (*model.Book, error)
	WriteCover(id int64, img []byte) error
	Delete(id int64) error
}

// Exporter produces the rsync-export rendition of a book for the reader/ view.
// It is the single swap point between serving the original epub and a converted
// kepub: the Library returns the appropriate implementation based on config.
type Exporter interface {
	Open(*model.Book) (EpubReader, error) // bytes for reads
	Size(*model.Book) (int64, bool)       // cheap; 9P stat length, false when cold
	Warm(*model.Book)                     // non-blocking proactive warm hint
	Filename(*model.Book) string          // FAT-safe export name
	Dirname(*model.Book) string           // FAT-safe export directory name
	Statuses() []string                   // which book statuses appear in the reader view
}

func Open(cfg config.LibraryConfig, forceReindex bool) (Library, error) {
	if err := os.MkdirAll(cfg.Root, 0755); err != nil {
		return nil, fmt.Errorf("creating library root: %w", err)
	}
	if err := os.MkdirAll(cfg.InboxTemp, 0700); err != nil {
		return nil, fmt.Errorf("creating inbox temp dir: %w", err)
	}
	if err := cleanInboxTemp(cfg.InboxTemp); err != nil {
		return nil, fmt.Errorf("cleaning inbox temp: %w", err)
	}
	if err := checkSameFilesystem(cfg.Root, cfg.InboxTemp); err != nil {
		return nil, fmt.Errorf("inbox_temp must be on the same filesystem as library.root: %w", err)
	}

	idx, err := index.Open(cfg.IndexPath)
	if err != nil {
		return nil, err
	}
	lib := &libraryImpl{
		store:     store.New(cfg.Root, cfg.InboxTemp),
		index:     idx,
		inboxTemp: cfg.InboxTemp,
		bookMu:    make(map[int64]*sync.Mutex),
	}
	if forceReindex || lib.needsReindex() {
		if err := lib.Reindex(); err != nil {
			return nil, fmt.Errorf("reindexing library: %w", err)
		}
	} else {
		log.Printf("reindex: index is clean, skipping")
	}
	return lib, nil
}

func cleanInboxTemp(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".epub") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if err := os.Remove(path); err != nil {
			log.Printf("warning: removing stale inbox temp %q: %v", path, err)
		} else {
			log.Printf("removed stale inbox temp %q", path)
		}
	}
	return nil
}

// checkSameFilesystem verifies that a and b are on the same mount, which ingest
// relies on: the frontend writes to a temp file inside inboxTemp then atomically
// renames it into the library root, and rename only works within a filesystem.
func checkSameFilesystem(a, b string) error {
	tmp, err := os.CreateTemp(a, ".fschk-*")
	if err != nil {
		return err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	dst := filepath.Join(b, filepath.Base(tmp.Name()))
	if err := os.Rename(tmp.Name(), dst); err != nil {
		os.Remove(dst)
		return err
	}
	os.Remove(dst)
	return nil
}
