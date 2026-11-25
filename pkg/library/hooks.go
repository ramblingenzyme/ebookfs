package library

import (
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"sync"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
)

// Hook is a unified interface for observing and amending library operations.
// Hooks embed HookBase and override the phases they care about.
//
// Setup runs once during library initialization, before any other phases.
// It receives paths the hook needs for persistent state.
//
// Pipeline phases (PreParse, PreCommit) run during ingest and can modify data
// before the library commits. If they return an error, the ingest fails.
// PreParse runs concurrently with other uploads. PreCommit runs one ingest at
// a time.
//
// PreParse receives a tempDir for scratch space. If PreParse returns a path
// inside tempDir (e.g., after format conversion), that file persists through
// PreCommit and the ingest operation. The library cleans up tempDir after
// the ingest completes (success or failure).
//
// Event phases (OnIngested, OnEdited, OnDeleting, OnDeleted) fire under the
// per-book lock. OnDeleting fires before deletion; the others fire after the
// operation commits. They must not block or call back into the library.
// Panics are recovered and logged so one hook cannot prevent later hooks
// from receiving the event.
type Hook interface {
	// Setup initializes the hook with library paths. Called by AddHook when the
	// hook is registered, before any other hook phases.
	Setup(root string, stateDir string) error

	// PreParse rewrites the epub file before parsing. epubPath is the absolute
	// path to the staged file. tempDir is per-ingest scratch space, retained
	// until ingest completes. Return a replacement path, or "" to keep epubPath.
	PreParse(epubPath string, tempDir string) (string, error)

	// PreCommit amends the bibliographic record before committing. The hook
	// modifies bib in place, and the library writes the changes into the epub,
	// so a later reindex keeps them.
	//
	// Pubdate, Identifiers, CoverPath, OpfSize and CoverSize cannot be written
	// to the epub, and changing one fails the ingest. An amendment an edit
	// would refuse, such as an empty title, also fails it.
	PreCommit(bib *Bib) error

	// OnIngested fires after a book is ingested, under the per-book lock.
	// The hook receives the book, a sidecar root scoped to .sidecar/, and a
	// lazy OpenEpub function that opens the epub for reading.
	OnIngested(book *Book, sidecars *os.Root, openEpub func() (EpubReader, error))

	// OnEdited fires after a book is edited, under the per-book lock.
	OnEdited(book *Book, sidecars *os.Root, openEpub func() (EpubReader, error))

	// OnDeleting fires before a book is deleted, under the per-book lock.
	// The hook receives the book, a sidecar root scoped to .sidecar/, and a
	// lazy OpenEpub function that opens the epub for reading. The epub is
	// deleted from disk after all OnDeleting hooks return.
	OnDeleting(book *Book, sidecars *os.Root, openEpub func() (EpubReader, error))

	// OnDeleted fires after a book is deleted, under the per-book lock.
	OnDeleted(bookID int64)
}

// HookBase provides no-op defaults for all hook phases. Hooks embed HookBase
// and override only the methods they need.
type HookBase struct{}

func (HookBase) Setup(root string, stateDir string) error { return nil }

func (HookBase) PreParse(epubPath string, tempDir string) (string, error) {
	return "", nil
}

func (HookBase) PreCommit(bib *Bib) error { return nil }

func (HookBase) OnIngested(book *Book, sidecars *os.Root, openEpub func() (EpubReader, error)) {
}

func (HookBase) OnEdited(book *Book, sidecars *os.Root, openEpub func() (EpubReader, error)) {}

func (HookBase) OnDeleting(book *Book, sidecars *os.Root, openEpub func() (EpubReader, error)) {}

func (HookBase) OnDeleted(bookID int64) {}

type hookSet struct {
	mu    sync.RWMutex
	hooks []Hook
}

func (s *hookSet) add(h Hook) {
	s.mu.Lock()
	s.hooks = append(slices.Clone(s.hooks), h)
	s.mu.Unlock()
}

func (s *hookSet) snapshot() []Hook {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.hooks)
}

// AddHook calls Setup with the library's root and stateDir, then registers the
// hook. A Setup error or panic returns an error and leaves the hook unregistered,
// so it never hears an event with its state uninitialised.
func (l *Library) AddHook(h Hook) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("hook %T setup panicked: %v", h, r)
		}
	}()
	if err := h.Setup(l.store.Root(), l.stateDir); err != nil {
		return fmt.Errorf("hook %T setup: %w", h, err)
	}
	l.hooks.add(h)
	return nil
}

// preParse calls PreParse in registration order. An error stops the ingest;
// an empty returned path keeps the path passed to that hook.
func (s *hookSet) preParse(epubPath string, tempDir string) (string, error) {
	currentPath := epubPath
	for _, h := range s.snapshot() {
		newPath, err := h.PreParse(currentPath, tempDir)
		if err != nil {
			return "", err
		}
		if newPath != "" {
			currentPath = newPath
		}
	}
	return currentPath, nil
}

// preCommit calls PreCommit in registration order on a copy of parsed and
// stops on the first error. It writes what the hooks changed into the epub at
// epubPath and returns the record read back from the file, so the index holds
// what a rebuild would derive.
func (s *hookSet) preCommit(epubPath string, parsed *Bib) (*Bib, error) {
	amended := cloneBib(*parsed)
	for _, h := range s.snapshot() {
		if err := h.PreCommit(&amended); err != nil {
			return nil, err
		}
	}
	e, err := bibEdits(*parsed, amended)
	if err != nil {
		return nil, err
	}
	bib, err := epub.Rewrite(epubPath, &book.Book{Bib: *parsed}, e)
	if err != nil {
		return nil, err
	}
	return &bib, nil
}

// cloneBib copies every slice, map and pointer, so a hook editing in place
// cannot reach the record bibEdits compares against.
func cloneBib(b book.Bib) book.Bib {
	b.Authors = slices.Clone(b.Authors)
	b.Subjects = slices.Clone(b.Subjects)
	b.Contributors = slices.Clone(b.Contributors)
	b.Identifiers = maps.Clone(b.Identifiers)
	if b.Series != nil {
		s := *b.Series
		b.Series = &s
	}
	return b
}

// bibEdits returns the Edits that turn from into to. A change to a field the
// epub cannot be written through fails, since indexing it would hold a value
// the next rebuild drops.
func bibEdits(from, to book.Bib) (book.Edits, error) {
	for _, f := range []struct {
		name string
		same bool
	}{
		{"pubdate", from.Pubdate == to.Pubdate},
		{"identifiers", maps.Equal(from.Identifiers, to.Identifiers)},
		{"cover path", from.CoverPath == to.CoverPath},
		{"package document size", from.OpfSize == to.OpfSize},
		{"cover size", from.CoverSize == to.CoverSize},
	} {
		if !f.same {
			return book.Edits{}, fmt.Errorf("%s cannot be written to the epub", f.name)
		}
	}

	changed := func(from, to string) *string {
		if from == to {
			return nil
		}
		return &to
	}
	e := book.Edits{
		Title:       changed(from.Title, to.Title),
		SortTitle:   changed(from.SortTitle, to.SortTitle),
		Description: changed(from.Description, to.Description),
		Language:    changed(from.Language, to.Language),
		Publisher:   changed(from.Publisher, to.Publisher),
		Rights:      changed(from.Rights, to.Rights),
	}
	if !slices.Equal(from.Authors, to.Authors) {
		e.Authors = &to.Authors
	}
	if !slices.Equal(from.Subjects, to.Subjects) {
		e.Subjects = &to.Subjects
	}
	if !slices.Equal(from.Contributors, to.Contributors) {
		e.Contributors = &to.Contributors
	}
	if seriesKey(from.Series) != seriesKey(to.Series) {
		removed := ""
		e.Series = &removed
		if to.Series != nil {
			e.Series, e.SeriesIndex = &to.Series.Name, &to.Series.Index
		}
	}
	return e, nil
}

// seriesKey leaves out the index-assigned ID, which a parse never sets.
func seriesKey(s *book.SeriesRef) [2]string {
	if s == nil {
		return [2]string{}
	}
	return [2]string{s.Name, s.Index}
}

func (s *hookSet) dispatchEvent(l *Library, b *Book, loc book.Location, name string, call func(Hook, *os.Root, func() (EpubReader, error))) {
	hooks := s.snapshot()
	if len(hooks) == 0 {
		return
	}
	root, err := l.store.OpenSidecars(loc)
	if err != nil {
		slog.Error("hook failed to open sidecars", "hook", name, "book_id", b.ID(), "error", err)
		return
	}
	defer root.Close()
	openEpub := func() (EpubReader, error) { return l.Content(b.ID()) }
	for _, h := range hooks {
		invokeEvent(name, h, func() { call(h, root, openEpub) })
	}
}

func (s *hookSet) onIngested(l *Library, b *Book, loc book.Location) {
	s.dispatchEvent(l, b, loc, "OnIngested", func(h Hook, root *os.Root, openEpub func() (EpubReader, error)) {
		h.OnIngested(b, root, openEpub)
	})
}

func (s *hookSet) onEdited(l *Library, b *Book, loc book.Location) {
	s.dispatchEvent(l, b, loc, "OnEdited", func(h Hook, root *os.Root, openEpub func() (EpubReader, error)) {
		h.OnEdited(b, root, openEpub)
	})
}

func (s *hookSet) onDeleting(l *Library, b *Book, loc book.Location) {
	s.dispatchEvent(l, b, loc, "OnDeleting", func(h Hook, root *os.Root, openEpub func() (EpubReader, error)) {
		h.OnDeleting(b, root, openEpub)
	})
}

func (s *hookSet) onDeleted(bookID int64) {
	for _, h := range s.snapshot() {
		invokeEvent("OnDeleted", h, func() { h.OnDeleted(bookID) })
	}
}

func invokeEvent(name string, h Hook, call func()) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("hook panicked", "event", name, "hook", h, "panic", r)
		}
	}()
	call()
}
