package library

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
)

// IngestHandle stages an epub upload. The frontend writes the bytes, then
// calls Ingest, which closes the file, parses the epub, lays it down in the
// store and removes the temp file.
type IngestHandle interface {
	io.WriterAt
	Ingest() (*Book, error)
}

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

func (l *Library) CreateIngest() (IngestHandle, error) {
	f, err := os.CreateTemp(l.inboxTemp, "*.epub")
	if err != nil {
		return nil, err
	}
	return &ingestHandle{file: f, ingestFn: l.ingestPath}, nil
}

func (l *Library) ingestPath(epubPath string) (*Book, error) {
	// Parse before taking ingestMu: it touches only this upload's staged temp
	// file, so bulk uploads overlap their parsing instead of serializing on it.
	bib, err := epub.Parse(epubPath)
	if err != nil {
		return nil, err
	}

	l.mutateMu.RLock()
	defer l.mutateMu.RUnlock()

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
			// The cleanup succeeded, so no on-disk state is left to heal.
			op.Cancel()
		} else {
			slog.Error("ingest cleanup failed", "path", loc.Dir(), "error", rmErr)
		}

		return nil, err
	}

	slog.Info("ingest: book added", "book_id", b.ID(), "title", b.Title(), "authors", book.JoinAuthors(b.Authors(), ", "))
	return b, nil
}

// authorNames is the set Index.Exists compares against. It filters nothing:
// the set compared has to be the set written, or the same book ingests twice.
// Nothing needs filtering either, since epub.Parse drops creators that
// sanitize to nothing and rejects a book left with none, and Edits rejects an
// empty name.
func authorNames(authors []book.Author) []string {
	names := make([]string, len(authors))
	for i, a := range authors {
		names[i] = a.Name
	}
	slices.Sort(names)
	return slices.Compact(names)
}
