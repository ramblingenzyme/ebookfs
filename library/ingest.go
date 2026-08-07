package library

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// IngestHandle is a writable handle for staging an epub upload, returned by
// Library.CreateIngest. The frontend writes upload bytes via WriteAt, then
// calls Ingest to finalize: the file is closed, the epub is parsed and laid
// down in the store, and the temp file is cleaned up.
type IngestHandle interface {
	io.WriterAt // WriteAt(p []byte, off int64) (int, error)
	Ingest() (*model.Book, error)
}

// ingestHandle is the concrete file-backed implementation of IngestHandle.
type ingestHandle struct {
	file     *os.File
	ingestFn func(string) (*model.Book, error)
}

func (h *ingestHandle) WriteAt(p []byte, off int64) (int, error) { return h.file.WriteAt(p, off) }

func (h *ingestHandle) Ingest() (*model.Book, error) {
	path := h.file.Name()
	if err := h.file.Close(); err != nil {
		slog.Warn("ingest: close temp file failed", "path", path, "error", err)
	}
	b, err := h.ingestFn(path)
	if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
		slog.Warn("ingest: remove temp file failed", "path", path, "error", rmErr)
	}
	return b, err
}

/* Library methods */
func (l *libraryImpl) CreateIngest() (IngestHandle, error) {
	f, err := os.CreateTemp(l.inboxTemp, "*.epub")
	if err != nil {
		return nil, err
	}
	return &ingestHandle{file: f, ingestFn: l.ingestPath}, nil
}

// ingestPath parses the staged epub, lays it down in the store, and records it
// in the index.
func (l *libraryImpl) ingestPath(epubPath string) (*model.Book, error) {
	// Parse before taking ingestMu: it touches only this upload's staged temp
	// file, so bulk uploads overlap their parsing instead of serializing on it.
	bib, err := epub.Parse(epubPath)
	if err != nil {
		return nil, err
	}

	if bib.Title == "" {
		return nil, fmt.Errorf("epub has no title")
	}
	if len(bib.Authors) == 0 {
		bib.Authors = []model.Author{{Name: model.UnknownAuthor, SortName: model.UnknownAuthor}}
	}

	l.ingestMu.Lock()
	defer l.ingestMu.Unlock()

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

	op := l.index.BeginOp()
	if err := op.MarkPending(); err != nil {
		return nil, err
	}

	cleanup := func() {
		if rmErr := l.store.Delete(loc); rmErr != nil {
			slog.Error("ingest cleanup failed", "path", loc.Dir(), "error", rmErr)
		} else {
			// Clean up: nothing was written to disk, so there is no state to heal.
			op.Cancel()
		}
	}

	mt, err := l.store.Ingest(epubPath, loc, &meta)
	if err != nil {
		cleanup()
		return nil, err
	}

	b := bookFromBib(*bib, meta, loc, mt)
	if err := op.Put(b, mt); err != nil {
		cleanup()
		return nil, err
	}

	slog.Info("ingest: book added", "book_id", b.Meta.ID, "title", b.Title, "authors", model.JoinAuthors(bib.Authors, ", "))
	return b, nil
}
