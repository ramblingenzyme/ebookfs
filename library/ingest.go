package library

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// IngestHandle is a writable handle for staging an epub upload, returned by
// Library.CreateIngest. The frontend writes upload bytes via WriteAt, then
// calls Ingest to finalize: the file is closed, the epub is parsed and laid
// down in the store, and the temp file is cleaned up.
type IngestHandle interface {
	io.WriterAt // WriteAt(p []byte, off int64) (int, error)
	Ingest() (*Book, error)
}

// ingestHandle is the concrete file-backed implementation of IngestHandle.
type ingestHandle struct {
	file     *os.File
	ingestFn func(string) (*Book, error)
}

func (h *ingestHandle) WriteAt(p []byte, off int64) (int, error) { return h.file.WriteAt(p, off) }

func (h *ingestHandle) Ingest() (*Book, error) {
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
func (l *libraryImpl) ingestPath(epubPath string) (*Book, error) {
	// Parse before taking ingestMu: it touches only this upload's staged temp
	// file, so bulk uploads overlap their parsing instead of serializing on it.
	bib, err := epub.Parse(epubPath)
	if err != nil {
		return nil, err
	}

	l.ingestMu.Lock()
	defer l.ingestMu.Unlock()

	dupe, err := l.index.Exists(bib.Title, authorNames(bib.Authors))
	if err != nil {
		return nil, err
	}
	if dupe {
		return nil, fmt.Errorf("%q: %w", bib.Title, ErrDuplicate)
	}
	// The index answers for books it holds; it cannot answer for one it skipped.
	if l.store.PathTaken(bib.Authors, bib.Title) {
		return nil, fmt.Errorf("%q: %w", bib.Title, ErrDuplicateOnDisk)
	}

	id, err := l.index.NextID()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	meta := book.Meta{
		ID:           id,
		DateAdded:    now,
		DateModified: now,
	}
	loc := l.store.Layout(bib.Authors, bib.Title, id)

	op := l.index.BeginOp()
	if err := op.MarkPending(); err != nil {
		return nil, err
	}

	b, err := func() (*Book, error) {
		mt, err := l.store.Ingest(epubPath, loc, &meta)
		if err != nil {
			return nil, err
		}

		b := bookFromBib(*bib, meta, loc, mt)
		if err := op.Put(b, mt); err != nil {
			return nil, err
		}

		return book.NewImmutableBook(b), nil
	}()

	if err != nil {
		if rmErr := l.store.Delete(loc); rmErr == nil {
			// Clean up: nothing was written to disk, so there is no state to heal.
			op.Cancel()
		} else {
			slog.Error("ingest cleanup failed", "path", loc.Dir(), "error", rmErr)
		}

		return nil, err
	}

	slog.Info("ingest: book added", "book_id", b.ID(), "title", b.Title(), "authors", model.JoinAuthors(b.Authors(), ", "))
	return b, nil
}

// authorNames returns the authors' distinct display names, the set Index.Exists
// compares against. Names are non-empty by construction — epub.Parse drops
// creators whose name sanitizes to nothing and rejects a book left with none,
// and Edits rejects an empty author name — so there is nothing to filter here.
// Filtering would be wrong anyway: the set compared has to be the set written,
// or the same book ingests twice.
func authorNames(authors []book.Author) []string {
	names := make([]string, len(authors))
	for i, a := range authors {
		names[i] = a.Name
	}
	slices.Sort(names)
	return slices.Compact(names)
}
