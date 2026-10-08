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
// calls Ingest, which files the book and removes the temporary file.
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
	// Create a temp directory for PreParse hooks. returned path from hooks can be inside,
	// so it can't be cleaned up until after ingest completes.
	hookTempDir, err := os.MkdirTemp(l.inboxTemp, "hooks-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(hookTempDir)

	// Run PreParse hooks before parsing
	processedPath, err := l.hooks.preParse(epubPath, hookTempDir)
	if err != nil {
		return nil, fmt.Errorf("pre-parse hook: %w", err)
	}

	// Parsed before ingestMu is taken, so bulk uploads parse in parallel.
	bib, err := epub.Parse(processedPath)
	if err != nil {
		return nil, err
	}

	l.mutateMu.RLock()
	defer l.mutateMu.RUnlock()

	l.ingestMu.Lock()
	defer l.ingestMu.Unlock()

	// Hooks may amend the bibliographic fields used for duplicate detection
	// and filing, so run them before checking those fields.
	if err := l.hooks.preCommit(bib); err != nil {
		return nil, fmt.Errorf("pre-commit hook: %w", err)
	}

	dupe, err := l.index.Exists(bib.Title, authorNames(bib.Authors))
	if err != nil {
		return nil, err
	}
	if dupe {
		return nil, fmt.Errorf("%q: %w", bib.Title, ErrDuplicate)
	}
	// The index cannot see a book it skipped.
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
		mt, err := l.store.Ingest(processedPath, loc, &meta)
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
			// Nothing is left on disk to heal.
			op.Cancel()
		} else {
			slog.Error("ingest cleanup failed", "path", loc.Dir(), "error", rmErr)
		}

		return nil, err
	}

	slog.Info("ingest: book added", "book_id", b.ID(), "title", b.Title(), "authors", book.JoinAuthors(b.Authors(), ", "))

	// Serialize the event with later edits and deletes of this newly indexed book.
	mu := l.bookMu.For(b.ID())
	mu.Lock()
	l.hooks.onIngested(l, b, loc)
	mu.Unlock()

	return b, nil
}

// authorNames must not filter: the set Index.Exists compares has to be the set
// written, or the same book ingests twice. Empty names are rejected before
// this anyway.
func authorNames(authors []book.Author) []string {
	names := make([]string, len(authors))
	for i, a := range authors {
		names[i] = a.Name
	}
	slices.Sort(names)
	return slices.Compact(names)
}
