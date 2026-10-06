package library

import (
	"log/slog"
	"os"
	"slices"
	"sync"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

// Hook is a unified interface for observing and amending library operations.
// Hooks embed HookBase and override the phases they care about.
//
// Pipeline phases (PreParse, PreCommit) run during ingest under ingestMu.
// They can modify data before the library commits. If they return an error,
// the operation fails.
//
// PreParse receives a tempDir for scratch space. If PreParse returns a path
// inside tempDir (e.g., after format conversion), that file persists through
// PreCommit and the ingest operation. The library cleans up tempDir after
// the ingest completes (success or failure).
//
// Event phases (OnIngested, OnEdited, OnDeleted) fire after the operation
// commits, under the per-book lock. They must not block or call back into
// the library. Panics are recovered and logged so one hook cannot prevent
// later hooks from receiving the event.
type Hook interface {
	// PreParse rewrites the epub file before parsing. epubPath is the absolute
	// path to the staged file. tempDir is per-ingest scratch space, retained
	// until ingest completes. Return a replacement path, or "" to keep epubPath.
	PreParse(epubPath string, tempDir string) (string, error)

	// PreCommit amends the bibliographic record before committing. The hook
	// modifies bib in place.
	PreCommit(bib *Bib) error

	// OnIngested fires after a book is ingested, under the per-book lock.
	// The hook receives the book, a sidecar root scoped to .sidecar/, and a
	// lazy OpenEpub function that opens the epub for reading.
	OnIngested(book *Book, sidecars *os.Root, openEpub func() (EpubReader, error))

	// OnEdited fires after a book is edited, under the per-book lock.
	OnEdited(book *Book, sidecars *os.Root, openEpub func() (EpubReader, error))

	// OnDeleted fires after a book is deleted, under the per-book lock.
	OnDeleted(bookID int64)
}

// HookBase provides no-op defaults for all hook phases. Hooks embed HookBase
// and override only the methods they need.
type HookBase struct{}

func (HookBase) PreParse(epubPath string, tempDir string) (string, error) {
	return "", nil
}

func (HookBase) PreCommit(bib *Bib) error { return nil }

func (HookBase) OnIngested(book *Book, sidecars *os.Root, openEpub func() (EpubReader, error)) {
}

func (HookBase) OnEdited(book *Book, sidecars *os.Root, openEpub func() (EpubReader, error)) {}

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

// AddHook registers a hook. The hook is called synchronously at each phase,
// in registration order. Hooks stay registered for the library's lifetime.
func (l *Library) AddHook(h Hook) { l.hooks.add(h) }

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

// preCommit calls PreCommit in registration order and stops on the first error.
func (s *hookSet) preCommit(bib *Bib) error {
	for _, h := range s.snapshot() {
		if err := h.PreCommit(bib); err != nil {
			return err
		}
	}
	return nil
}

func (s *hookSet) onIngested(l *Library, b *Book, loc book.Location) {
	hooks := s.snapshot()
	if len(hooks) == 0 {
		return
	}
	root, err := l.store.OpenSidecars(loc)
	if err != nil {
		slog.Error("hook OnIngested: failed to open sidecars", "book_id", b.ID(), "error", err)
		return
	}
	defer root.Close()
	openEpub := func() (EpubReader, error) { return l.Content(b.ID()) }
	for _, h := range hooks {
		invokeEvent("OnIngested", h, func() { h.OnIngested(b, root, openEpub) })
	}
}

func (s *hookSet) onEdited(l *Library, b *Book, loc book.Location) {
	hooks := s.snapshot()
	if len(hooks) == 0 {
		return
	}
	root, err := l.store.OpenSidecars(loc)
	if err != nil {
		slog.Error("hook OnEdited: failed to open sidecars", "book_id", b.ID(), "error", err)
		return
	}
	defer root.Close()
	openEpub := func() (EpubReader, error) { return l.Content(b.ID()) }
	for _, h := range hooks {
		invokeEvent("OnEdited", h, func() { h.OnEdited(b, root, openEpub) })
	}
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
