// Package library hook scenarios pin the public operation-to-hook contract,
// including the per-book lock held while an edit event is delivered.
package library_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

type ingestEditDeleteHook struct {
	library.HookBase
	t           *testing.T
	events      []string
	converted   string
	preCommitOK bool
}

func (h *ingestEditDeleteHook) PreParse(source, tempDir string) (string, error) {
	data, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	h.converted = filepath.Join(tempDir, "converted.epub")
	if err := os.WriteFile(h.converted, data, 0600); err != nil {
		return "", err
	}
	return h.converted, nil
}

func (h *ingestEditDeleteHook) PreCommit(bib *library.Bib) error {
	if _, err := os.Stat(h.converted); err != nil {
		return err
	}
	bib.Publisher = "Hook Publisher"
	h.preCommitOK = true
	h.events = append(h.events, "pre-commit")
	return nil
}

func (h *ingestEditDeleteHook) OnIngested(b *library.Book, sidecars *os.Root, openEpub func() (library.EpubReader, error)) {
	h.events = append(h.events, "ingested")
	if b.Publisher() != "Hook Publisher" {
		h.t.Errorf("OnIngested publisher = %q, want hook amendment", b.Publisher())
	}
	if err := sidecars.Mkdir("consumer", 0700); err != nil {
		h.t.Errorf("create sidecar namespace in OnIngested: %v", err)
		return
	}
	if err := sidecars.WriteFile("consumer/data", []byte("stored"), 0600); err != nil {
		h.t.Errorf("write sidecar in OnIngested: %v", err)
	}
	reader, err := openEpub()
	if err != nil {
		h.t.Errorf("open EPUB in OnIngested: %v", err)
		return
	}
	defer reader.Close()
	var first [4]byte
	if _, err := reader.ReadAt(first[:], 0); err != nil {
		h.t.Errorf("read EPUB in OnIngested: %v", err)
	}
}

func (h *ingestEditDeleteHook) OnEdited(*library.Book, *os.Root, func() (library.EpubReader, error)) {
	h.events = append(h.events, "edited")
}

func (h *ingestEditDeleteHook) OnDeleting(*library.Book, *os.Root, func() (library.EpubReader, error)) {
	h.events = append(h.events, "deleting")
}

func (h *ingestEditDeleteHook) OnDeleted(id int64) {
	h.events = append(h.events, "deleted")
}

func TestHookLifecycleUsesCommittedOperations(t *testing.T) {
	lib := openTestLibrary(t)
	hook := &ingestEditDeleteHook{t: t}
	lib.AddHook(hook)

	data := buildTestEpub(t, "Hooked Book", "Alice")
	b := ingestTestEpub(t, lib, data)
	if !hook.preCommitOK {
		t.Fatal("PreCommit did not run")
	}
	if _, err := os.Stat(hook.converted); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("PreParse output still exists after ingest: stat error = %v", err)
	}
	stored, err := lib.ReadSidecar(b.ID(), "consumer/data")
	if err != nil {
		t.Fatalf("read sidecar written by OnIngested: %v", err)
	}
	if string(stored) != "stored" {
		t.Errorf("sidecar = %q, want stored", stored)
	}

	rating := 3.0
	if _, err := lib.Edit(b.ID(), library.Edits{Rating: &rating}); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if err := lib.Delete(b.ID()); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	want := []string{"pre-commit", "ingested", "edited", "deleting", "deleted"}
	if len(hook.events) != len(want) {
		t.Fatalf("hook events = %v, want %v", hook.events, want)
	}
	for i := range want {
		if hook.events[i] != want[i] {
			t.Errorf("hook event %d = %q, want %q", i, hook.events[i], want[i])
		}
	}
}

type blockingEditHook struct {
	library.HookBase
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *blockingEditHook) OnEdited(b *library.Book, _ *os.Root, _ func() (library.EpubReader, error)) {
	if b.Title() != "First Edit" {
		return
	}
	h.once.Do(func() { close(h.entered) })
	<-h.release
}

func TestEditWaitsForItsHookBeforeAnotherEdit(t *testing.T) {
	lib := openTestLibrary(t)
	b := ingestTestEpub(t, lib, buildTestEpub(t, "Before", "Alice"))
	hook := &blockingEditHook{entered: make(chan struct{}), release: make(chan struct{})}
	lib.AddHook(hook)

	firstTitle := "First Edit"
	firstDone := make(chan error, 1)
	go func() {
		_, err := lib.Edit(b.ID(), library.Edits{Title: &firstTitle})
		firstDone <- err
	}()
	select {
	case <-hook.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first edit did not reach OnEdited")
	}

	secondTitle := "Second Edit"
	secondStarted := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		close(secondStarted)
		_, err := lib.Edit(b.ID(), library.Edits{Title: &secondTitle})
		secondDone <- err
	}()
	<-secondStarted
	select {
	case err := <-secondDone:
		t.Fatalf("second edit returned before first hook completed: %v", err)
	case <-time.After(30 * time.Millisecond):
	}

	close(hook.release)
	for name, done := range map[string]<-chan error{"first edit": firstDone, "second edit": secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
		case <-time.After(3 * time.Second):
			t.Errorf("%s did not finish after releasing OnEdited", name)
		}
	}
}

type failingIngestHook struct {
	library.HookBase
	preParseErr  error
	preCommitErr error
}

func (h failingIngestHook) PreParse(string, string) (string, error) {
	return "", h.preParseErr
}

func (h failingIngestHook) PreCommit(*library.Bib) error { return h.preCommitErr }

func TestIngestHookErrorsAbortIngest(t *testing.T) {
	for _, tc := range []struct {
		name string
		hook library.Hook
	}{
		{name: "pre-parse", hook: failingIngestHook{preParseErr: errors.New("pre-parse failed")}},
		{name: "pre-commit", hook: failingIngestHook{preCommitErr: errors.New("pre-commit failed")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lib := openTestLibrary(t)
			lib.AddHook(tc.hook)
			handle, err := lib.CreateIngest()
			if err != nil {
				t.Fatalf("CreateIngest: %v", err)
			}
			if _, err := handle.WriteAt(buildTestEpub(t, "Rejected", "Alice"), 0); err != nil {
				t.Fatalf("WriteAt: %v", err)
			}
			if _, err := handle.Ingest(); err == nil {
				t.Fatal("Ingest succeeded despite hook error")
			}
			books, err := lib.Search(library.Query{})
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if len(books) != 0 {
				t.Errorf("failed ingest left %d indexed books", len(books))
			}
		})
	}
}

type epubReadingHook struct {
	library.HookBase
	t            *testing.T
	epubReadable bool
}

func (h *epubReadingHook) OnDeleting(book *library.Book, sidecars *os.Root, openEpub func() (library.EpubReader, error)) {
	if openEpub == nil {
		return
	}
	reader, err := openEpub()
	if err != nil {
		return
	}
	defer reader.Close()

	// Try to read the epub header
	var header [4]byte
	_, err = reader.ReadAt(header[:], 0)
	if err != nil {
		return
	}

	// Check if it's a valid zip/epub (starts with PK)
	if header[0] == 'P' && header[1] == 'K' {
		h.epubReadable = true
	}
}

func TestOnDeletingHookCanReadEpub(t *testing.T) {
	lib := openTestLibrary(t)
	hook := &epubReadingHook{t: t}
	lib.AddHook(hook)

	data := buildTestEpub(t, "Readable Book", "Alice")
	b := ingestTestEpub(t, lib, data)

	if err := lib.Delete(b.ID()); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if !hook.epubReadable {
		t.Error("OnDeleting hook could not read epub content")
	}
}
