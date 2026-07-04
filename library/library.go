package library

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/internal/index"
	"github.com/ramblingenzyme/ebookfs/library/internal/naming"
	"github.com/ramblingenzyme/ebookfs/library/internal/store"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// EpubReader is a handle to a book's epub content. It hides where the bytes
// live (currently a file on disk) from the 9P layer, which needs random reads
// and a close.
type EpubReader = model.EpubReader

// Library defines the public API for filesystem and index operations on the
// book collection. The concrete implementation is unexported; construct via New.
type Library interface {
	Ingest(epubPath string) (*model.Book, error)
	ListAll() ([]*model.Book, error)
	Reindex() error
	OpenEpub(b *model.Book) (EpubReader, error)
	ExtractCover(b *model.Book) ([]byte, error)
	ExtractOPF(b *model.Book) ([]byte, error)
	Edit(b *model.Book, e model.Edits) (*model.Book, error)
	WriteCover(b *model.Book, img []byte) error
	Delete(b *model.Book) error
	Exporter() Exporter
}

// Exporter produces the rsync-export rendition of a book for the reader/ view.
// It is the single swap point between serving the original epub and a converted
// kepub: the Library returns the appropriate implementation based on config.
type Exporter interface {
	Open(*model.Book) (EpubReader, error) // bytes for reads
	Size(*model.Book) (int64, bool)       // cheap; 9P stat length, false when cold
	Ensure(*model.Book) error             // proactive warm hook
	Filename(*model.Book) string          // FAT-safe export name
}

func Open(cfg *config.Config) (Library, error) {
	root := cfg.Library.Root
	inboxTemp := cfg.Library.InboxTemp
	indexPath := cfg.Index.Path

	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("creating library root: %w", err)
	}
	if err := os.MkdirAll(inboxTemp, 0700); err != nil {
		return nil, fmt.Errorf("creating inbox temp dir: %w", err)
	}
	if err := cleanInboxTemp(inboxTemp); err != nil {
		return nil, fmt.Errorf("cleaning inbox temp: %w", err)
	}
	if err := checkSameFilesystem(root, inboxTemp); err != nil {
		return nil, fmt.Errorf("inbox_temp must be on the same filesystem as library.root: %w", err)
	}

	idx, err := index.Open(indexPath)
	if err != nil {
		return nil, err
	}
	lib := &libraryImpl{store: store.New(root, inboxTemp), index: idx, cfg: cfg}
	if err := lib.Reindex(); err != nil {
		return nil, fmt.Errorf("reindexing library: %w", err)
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

func ForFAT(s string) (string, error) { return naming.ForFAT(s) }


