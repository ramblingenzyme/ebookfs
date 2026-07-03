package library

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/library/internal/index"
	"github.com/ramblingenzyme/ebookfs/library/internal/kepub"
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

// libraryImpl is the concrete Library implementation.
type libraryImpl struct {
	store *store.Store
	index *index.Index
	cfg   *config.Config
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

// Ingest parses the staged epub, lays it down in the store, and records it
// in the index.
func (l *libraryImpl) Ingest(epubPath string) (*model.Book, error) {
	book, err := epub.Parse(epubPath)
	if err != nil {
		return nil, err
	}

	bib := bibFromEpub(book)
	if bib.Title == "" {
		return nil, fmt.Errorf("epub has no title")
	}
	if len(bib.Authors) == 0 {
		bib.Authors = []model.Author{{Name: "Unknown", SortName: "Unknown"}}
	}
	if l.store.Exists(bib.Authors, bib.Title) {
		return nil, fmt.Errorf("book already in library: %q", bib.Title)
	}

	id, err := l.index.NextID()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	meta := model.Meta{
		ID:           id,
		DateAdded:    now,
		DateModified: now,
	}
	loc := l.store.Layout(bib.Authors, bib.Title, id)
	b := model.NewBook(bib, meta, loc)

	if err := l.index.Put(b, func() error {
		return l.store.Ingest(epubPath, b.Location, &b.Meta)
	}); err != nil {
		// Index write failed after the store wrote; clean up so a retry starts fresh.
		_ = l.store.Delete(b.Location)
		return nil, err
	}

	return b, nil
}

func (l *libraryImpl) ListAll() ([]*model.Book, error) {
	books, err := l.index.ListAll()
	if err != nil {
		return nil, err
	}
	for _, b := range books {
		b.EpubPath = l.store.AbsPath(b.LibraryPath, b.EpubFilename)
	}
	return books, nil
}

// Reindex rebuilds the index from the store (the source of truth). Skips
// entirely (O(1)) when the dirty flag is clear. Books that can't be read are
// logged and skipped rather than failing the whole rebuild.
func (l *libraryImpl) Reindex() error {
	needs, err := l.index.NeedsReindex()
	if err != nil {
		log.Printf("reindex: could not check index state (%v), forcing rebuild", err)
		needs = true
	}
	if !needs {
		log.Printf("reindex: index is clean, skipping")
		return nil
	}

	entries, err := l.store.Walk()
	if err != nil {
		return err
	}

	var (
		books []*model.Book
		maxID int64
	)
	for _, e := range entries {
		meta, err := l.store.ReadMeta(e)
		if err != nil {
			log.Printf("reindex: skip %s: read meta: %v", e.LibraryPath, err)
			continue
		}
		if meta.ID > maxID {
			maxID = meta.ID
		}

		book, err := epub.Parse(e.EpubPath)
		if err != nil {
			log.Printf("reindex: skip %s: parse epub: %v", e.LibraryPath, err)
			continue
		}

		books = append(books, model.NewBook(bibFromEpub(book), *meta, e))
	}

	if err := l.index.Rebuild(books, maxID); err != nil {
		return err
	}
	log.Printf("reindex: indexed %d of %d books", len(books), len(entries))
	return nil
}

// OpenEpub returns a handle to b's epub content. The caller must close it.
func (l *libraryImpl) OpenEpub(b *model.Book) (EpubReader, error) {
	return l.store.OpenEpub(b.Location)
}

// ExtractCover returns the cover image bytes from b's epub.
func (l *libraryImpl) ExtractCover(b *model.Book) ([]byte, error) {
	return epub.ExtractCover(b.EpubPath, b.CoverPath)
}

// ExtractOPF returns the raw OPF XML bytes from b's epub.
func (l *libraryImpl) ExtractOPF(b *model.Book) ([]byte, error) {
	return epub.ExtractOPF(b.EpubPath)
}

// Edit applies edits to a book, persists everything, and returns the updated
// book. If the title or authors change, the book directory is moved.
func (l *libraryImpl) Edit(b *model.Book, e model.Edits) (*model.Book, error) {
	if v := e.Validate(b); v != nil {
		return nil, v
	}

	updated := b.Edit(e)

	if err := l.index.Put(updated, func() error {
		if e.HasBibEdits() {
			re, err := epub.WriteBib(b.EpubPath, e)
			if err != nil {
				return err
			}
			updated.Bib = bibFromEpub(re)
		}
		newLoc := l.store.Layout(updated.Authors, updated.Title, updated.Meta.ID)
		if newLoc != b.Location {
			if err := l.store.Move(b.Location, newLoc); err != nil {
				return err
			}
			updated.Location = newLoc
		}
		return l.store.WriteMeta(updated.Location, &updated.Meta)
	}); err != nil {
		return nil, err
	}

	return updated, nil
}

// WriteCover replaces the cover image in b's epub with img.
func (l *libraryImpl) WriteCover(b *model.Book, img []byte) error {
	_, err := epub.WriteCover(b.EpubPath, b.CoverPath, img)
	return err
}

// bibFromEpub converts a parsed epub.Book into a model.Bib.
func bibFromEpub(src *epub.Book) model.Bib {
	var series *model.SeriesRef
	if src.Series != "" {
		series = &model.SeriesRef{Name: src.Series, Index: src.SeriesIndex}
	}

	identifiers := make(map[string]string, len(src.Identifiers))
	for _, ident := range src.Identifiers {
		identifiers[ident.ID] = ident.Value
	}

	return model.Bib{
		Title:       src.Title,
		SortTitle:   src.SortTitle,
		Authors:     src.Authors,
		Series:      series,
		Language:    src.Language,
		Pubdate:     src.PubDate,
		Description: src.Description,
		Identifiers: identifiers,
		CoverPath:   src.CoverPath,
	}
}

func (l *libraryImpl) Delete(b *model.Book) error {
	// Store is authoritative; a ghost index row is cleaned up by reindex.
	return l.index.Delete(b.Meta.ID, func() error { return l.store.Delete(b.Location) })
}

type KepubCache struct {
	c *kepub.Cache
}

func NewKepubCache(dir string, lib Library) *KepubCache {
	return &KepubCache{c: kepub.NewCache(dir, lib)}
}

func (k *KepubCache) Open(b *model.Book) (EpubReader, error) { return k.c.Open(b) }
func (k *KepubCache) Size(b *model.Book) (int64, bool)      { return k.c.Size(b) }
func (k *KepubCache) Ensure(b *model.Book) error             { return k.c.Ensure(b) }
func (k *KepubCache) Filename(b *model.Book) string          { return k.c.Filename(b) }

// epubExporter is the convert=false passthrough: it serves the original epub
// straight from the library, with conversion warming as a no-op.
type epubExporter struct {
	lib Library
}

func (e epubExporter) Open(b *model.Book) (EpubReader, error) {
	return e.lib.OpenEpub(b)
}

func (e epubExporter) Size(b *model.Book) (int64, bool) {
	fi, err := b.Stat()
	if err != nil {
		return 0, false
	}
	return fi.Size(), true
}

func (e epubExporter) Ensure(*model.Book) error { return nil }

func (e epubExporter) Filename(b *model.Book) string { return b.EpubFilename }

// Exporter returns the appropriate Exporter based on the reader config.
// If convert is enabled, it creates a kepub cache; otherwise it serves original epubs.
func (l *libraryImpl) Exporter() Exporter {
	if l.cfg.Reader.Convert {
		if err := os.MkdirAll(l.cfg.Reader.CacheDir, 0755); err != nil {
			log.Fatalf("creating kepub cache dir: %v", err)
		}
		return &KepubCache{c: kepub.NewCache(l.cfg.Reader.CacheDir, l)}
	}
	return epubExporter{lib: l}
}

func ForFAT(s string) (string, error) { return naming.ForFAT(s) }


