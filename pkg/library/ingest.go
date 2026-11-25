package library

import (
	"errors"
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
	defer func() {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("ingest: remove temp file failed", "path", path, "error", err)
		}
	}()
	// Synced here as well as in ingestPath, since the fd that wrote is the one
	// certain to see a writeback error.
	if err := h.file.Sync(); err != nil {
		h.file.Close()
		return nil, fmt.Errorf("syncing uploaded epub: %w", err)
	}
	// NFS can report a failed write as late as close.
	if err := h.file.Close(); err != nil {
		return nil, fmt.Errorf("closing uploaded epub: %w", err)
	}

	return h.ingestFn(path)
}

func (l *Library) CreateIngest() (IngestHandle, error) {
	f, err := os.CreateTemp(l.inboxTemp, "*.epub")
	if err != nil {
		return nil, err
	}

	return &ingestHandle{file: f, ingestFn: l.ingestPath}, nil
}

func (l *Library) ingestPath(epubPath string) (*Book, error) {
	// Removed only once ingest returns, since a PreParse hook may hand back a
	// file inside it.
	hookTempDir, err := os.MkdirTemp(l.inboxTemp, "hooks-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(hookTempDir)

	processedPath, err := l.hooks.preParse(epubPath, hookTempDir)
	if err != nil {
		return nil, fmt.Errorf("pre-parse hook: %w", err)
	}

	// Parsed before ingestMu is taken, so bulk uploads parse in parallel.
	parsed, err := epub.Parse(processedPath)
	if err != nil {
		return nil, err
	}

	// A PreParse hook can return a different file, which Ingest's fsync never saw.
	//
	// Outside ingestMu, since a slow fsync would stall other uploads.
	if err := syncFile(processedPath); err != nil {
		return nil, fmt.Errorf("syncing uploaded epub: %w", err)
	}

	l.mutateMu.RLock()
	defer l.mutateMu.RUnlock()

	l.ingestMu.Lock()
	defer l.ingestMu.Unlock()

	// Hooks may amend the bibliographic fields used for duplicate detection
	// and filing, so run them before checking those fields.
	bib, err := l.hooks.preCommit(processedPath, parsed)
	if err != nil {
		return nil, fmt.Errorf("pre-commit hook: %w", err)
	}

	if err := l.rejectDuplicate(bib); err != nil {
		return nil, err
	}

	id, err := l.allocateID()
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

	b, err := l.place(processedPath, bib, loc, meta)
	if err != nil {
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

func (l *Library) rejectDuplicate(bib *book.Bib) error {
	dupe, err := l.index.Exists(bib.Title, authorNames(bib.Authors))
	if err != nil {
		return err
	}

	if dupe {
		return fmt.Errorf("%q: %w", bib.Title, ErrDuplicate)
	}
	// The index cannot see a book it skipped.
	if l.store.PathTaken(bib.Authors, bib.Title) {
		return fmt.Errorf("%q: %w", bib.Title, ErrDuplicateOnDisk)
	}

	return nil
}

func (l *Library) allocateID() (int64, error) {
	id, err := l.index.NextID()
	if err != nil {
		return 0, err
	}

	if err := l.store.RecordID(id); err != nil {
		return 0, fmt.Errorf("recording book id %d: %w", id, err)
	}

	return id, nil
}

// place moves the epub to loc and indexes it, removing what it placed if the
// index write fails.
func (l *Library) place(processedPath string, bib *book.Bib, loc book.Location, meta book.Meta) (*Book, error) {
	op, err := l.index.MarkPending()
	if err != nil {
		return nil, err
	}

	b, err := func() (*Book, error) {
		meta, mt, err := l.store.Ingest(processedPath, loc, meta)
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

	return b, nil
}

func syncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return f.Sync()
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
