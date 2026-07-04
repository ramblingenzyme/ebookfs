package library

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/internal/index"
	"github.com/ramblingenzyme/ebookfs/library/internal/store"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// EpubReader is a handle to a book's epub content. It hides where the bytes
// live (currently a file on disk) from the 9P layer, which needs random reads
// and a close.
type EpubReader = model.EpubReader

// IngestHandle is a writable handle returned by Library.CreateIngest.
// The frontend writes upload bytes via WriteAt, then calls Ingest to
// finalize: the file is closed, the epub is parsed and laid down in the
// store, and the temp file is cleaned up. NewIngestHandle is exported for
// tests; production code uses CreateIngest.
type IngestHandle struct {
	file   *os.File
	path   string
	ingest func(string) (*model.Book, error)
}

func NewIngestHandle(f *os.File, path string, ingest func(string) (*model.Book, error)) *IngestHandle {
	return &IngestHandle{file: f, path: path, ingest: ingest}
}

func (h *IngestHandle) WriteAt(p []byte, off int64) (int, error) { return h.file.WriteAt(p, off) }

func (h *IngestHandle) Ingest() (*model.Book, error) {
	h.file.Close()
	if h.ingest == nil {
		return nil, nil
	}
	b, err := h.ingest(h.path)
	os.Remove(h.path)
	return b, err
}

// Library defines the public API for filesystem and index operations on the
// book collection. The concrete implementation is unexported; construct via New.
type Library interface {
	Close() error
	CreateIngest() (*IngestHandle, error)
	Exporter(config.ReaderConfig) (Exporter, error)
	Query(model.Filter) ([]*model.Book, error)
	Reindex() error
	OpenEpub(b *model.Book) (EpubReader, error)
	ExtractCover(b *model.Book) ([]byte, error)
	ExtractOPF(b *model.Book) ([]byte, error)
	Edit(b *model.Book, e model.Edits) (*model.Book, error)
	WriteCover(b *model.Book, img []byte) error
	Delete(b *model.Book) error
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
	lib := &libraryImpl{store: store.New(cfg.Root, cfg.InboxTemp), index: idx, inboxTemp: cfg.InboxTemp}
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


