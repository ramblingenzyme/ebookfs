package library

import (
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

// Hook is a unified interface for observing and amending library operations.
// Hooks embed HookBase and override the phases they care about.
//
// Setup runs once during library initialization, before any other phases.
// It receives paths the hook needs for persistent state.
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
// Event phases (OnIngested, OnEdited, OnDeleting, OnDeleted) fire under the
// per-book lock. OnDeleting fires before deletion; the others fire after the
// operation commits. They must not block or call back into the library.
// Panics are recovered and logged so one hook cannot prevent later hooks
// from receiving the event.
type Hook interface {
	// Setup initializes the hook with library paths. Called by AddHook when the
	// hook is registered, before any other hook phases.
	Setup(root string, stateDir string) error

	// PreParse rewrites the book file before parsing. srcPath is the absolute
	// path to the staged file, named with its detected format's extension when
	// the format is recognised. tempDir is per-ingest scratch space, retained
	// until ingest completes. Return a replacement path, or "" to keep srcPath.
	PreParse(srcPath string, tempDir string) (string, error)

	// PreCommit amends the bibliographic record before committing. The hook
	// modifies bib in place.
	PreCommit(bib *Bib) error

	// OnIngested fires after a book is ingested, under the per-book lock.
	// The hook receives the book, a sidecar root scoped to .sidecar/, and a
	// lazy openFile function that opens the book file for reading.
	OnIngested(book *Book, sidecars *os.Root, openFile func() (Reader, error))

	// OnEdited fires after a book is edited, under the per-book lock.
	OnEdited(book *Book, sidecars *os.Root, openFile func() (Reader, error))

	// OnDeleting fires before a book is deleted, under the per-book lock.
	// The hook receives the book, a sidecar root scoped to .sidecar/, and a
	// lazy openFile function that opens the book file for reading. The file is
	// deleted from disk after all OnDeleting hooks return.
	OnDeleting(book *Book, sidecars *os.Root, openFile func() (Reader, error))

	// OnDeleted fires after a book is deleted, under the per-book lock.
	OnDeleted(bookID int64)
}

// HookBase provides no-op defaults for all hook phases. Hooks embed HookBase
// and override only the methods they need.
type HookBase struct{}

func (HookBase) Setup(root string, stateDir string) error { return nil }

func (HookBase) PreParse(srcPath string, tempDir string) (string, error) {
	return "", nil
}

func (HookBase) PreCommit(bib *Bib) error { return nil }

func (HookBase) OnIngested(book *Book, sidecars *os.Root, openFile func() (Reader, error)) {
}

func (HookBase) OnEdited(book *Book, sidecars *os.Root, openFile func() (Reader, error)) {}

func (HookBase) OnDeleting(book *Book, sidecars *os.Root, openFile func() (Reader, error)) {}

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
func (s *hookSet) preParse(srcPath string, tempDir string) (string, error) {
	currentPath := srcPath
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

func (s *hookSet) dispatchEvent(l *Library, b *Book, loc book.File, name string, call func(Hook, *os.Root, func() (Reader, error))) {
	hooks := s.snapshot()
	if len(hooks) == 0 {
		return
	}
	root, err := l.store.OpenSidecars(loc)
	if err != nil {
		slog.Error("hook "+name+": failed to open sidecars", "book_id", b.ID(), "error", err)
		return
	}
	defer root.Close()
	openFile := func() (Reader, error) { return l.Content(b.ID()) }
	for _, h := range hooks {
		invokeEvent(name, h, func() { call(h, root, openFile) })
	}
}

func (s *hookSet) onIngested(l *Library, b *Book, loc book.File) {
	s.dispatchEvent(l, b, loc, "OnIngested", func(h Hook, root *os.Root, openFile func() (Reader, error)) {
		h.OnIngested(b, root, openFile)
	})
}

func (s *hookSet) onEdited(l *Library, b *Book, loc book.File) {
	s.dispatchEvent(l, b, loc, "OnEdited", func(h Hook, root *os.Root, openFile func() (Reader, error)) {
		h.OnEdited(b, root, openFile)
	})
}

func (s *hookSet) onDeleting(l *Library, b *Book, loc book.File) {
	s.dispatchEvent(l, b, loc, "OnDeleting", func(h Hook, root *os.Root, openFile func() (Reader, error)) {
		h.OnDeleting(b, root, openFile)
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
