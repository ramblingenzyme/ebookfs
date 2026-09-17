package library_test

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/library"
)

func TestLibraryImplExporter(t *testing.T) {
	lib := openTestLibrary(t)

	e, err := lib.Exporter(library.ReaderConfig{
		Statuses: []string{"unread"},
	})
	if err != nil {
		t.Fatalf("Exporter: %v", err)
	}
	if e == nil {
		t.Fatal("Exporter returned nil")
	}
	book := makeBook(1, "Test", "Author")
	book.Meta.Status = "unread"
	if !e.Includes(testutil.WrapBook(book)) {
		t.Errorf("Includes should return true for a book with status %q", book.Meta.Status)
	}
	book.Meta.Status = "read"
	if e.Includes(testutil.WrapBook(book)) {
		t.Errorf("Includes should return false for a book with status %q", book.Meta.Status)
	}
}

func TestLibraryImplExporterConvertRequiresCacheDir(t *testing.T) {
	lib := openTestLibrary(t)

	e, err := lib.Exporter(library.ReaderConfig{
		Statuses: []string{"unread", "reading"},
		Convert:  true,
		CacheDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Exporter with convert: %v", err)
	}
	if e == nil {
		t.Fatal("Exporter returned nil")
	}
}

// ReaderConfig's invariants are enforced by the package that owns the struct,
// not only by whatever parsed the values. A caller building one in Go gets the
// same checks a TOML file does.
func TestExporterRejectsBadReaderConfig(t *testing.T) {
	t.Run("convert without a cache dir", func(t *testing.T) {
		lib := openTestLibrary(t)
		if _, err := lib.Exporter(library.ReaderConfig{Convert: true}); err == nil {
			t.Error("Exporter accepted convert with no cache dir")
		}
	})

	t.Run("cache dir inside the library root", func(t *testing.T) {
		cfg := testConfig(t)
		lib := openLib(t, cfg)
		inside := filepath.Join(cfg.Root, "kepub-cache")
		if _, err := lib.Exporter(library.ReaderConfig{Convert: true, CacheDir: inside}); err == nil {
			t.Error("Exporter accepted a cache dir the store walk would index")
		}
	})

	t.Run("cache dir outside the library root", func(t *testing.T) {
		cfg := testConfig(t)
		lib := openLib(t, cfg)
		if _, err := lib.Exporter(library.ReaderConfig{Convert: true, CacheDir: t.TempDir()}); err != nil {
			t.Errorf("Exporter rejected a valid cache dir: %v", err)
		}
	})
}

// The kepub rendition forwards to its cache rather than answering for itself: a
// .kepub.epub name, the author dirname, a cold size for a book never converted,
// and an Open that surfaces the conversion's error. Close goes through
// lib.Close, which owns the exporters it hands out.
func TestKepubCacheDelegates(t *testing.T) {
	lib := openTestLibrary(t)
	exp, err := lib.Exporter(library.ReaderConfig{
		Statuses: []string{"reading"},
		Convert:  true,
		CacheDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Exporter: %v", err)
	}

	b := makeBook(1, "Test", "Alice")
	b.EpubPath = "mybook.epub"

	b.Meta.Status = "reading"
	if !exp.Includes(testutil.WrapBook(b)) {
		t.Error("Includes should be true for the configured status")
	}
	b.Meta.Status = "read"
	if exp.Includes(testutil.WrapBook(b)) {
		t.Error("Includes should be false for an unconfigured status")
	}

	if fn := exp.Filename(testutil.WrapBook(b)); fn != "mybook.kepub.epub" {
		t.Errorf("Filename = %q, want %q", fn, "mybook.kepub.epub")
	}

	if dn := exp.Dirname(testutil.WrapBook(b)); dn != "Alice" {
		t.Errorf("Dirname = %q, want %q", dn, "Alice")
	}

	if _, ok := exp.Size(testutil.WrapBook(b)); ok {
		t.Error("Size should report cold for a book with no cached conversion")
	}

	// Warm must not panic.
	exp.Warm(testutil.WrapBook(b))

	// The book is not in this library, so the conversion has no epub to read.
	if _, err := exp.Open(testutil.WrapBook(b)); err == nil {
		t.Error("expected an error opening a book the library does not hold")
	}

	// Close stops the warmer without error.
	if err := lib.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

// The read path the reader/ view is for. Everything else about the epub
// rendition (Includes, Filename, Dirname, Size) answers from the book record
// without touching disk, so nothing previously opened one. A mount that lists
// the right names and serves nothing would have passed every other test here.
func TestExporterOpenServesTheRealEpub(t *testing.T) {
	lib := openTestLibrary(t)
	b := ingestTestEpub(t, lib, buildTestEpub(t, "Readable", "Alice"))

	exp, err := lib.Exporter(library.ReaderConfig{Statuses: []string{"unread"}})
	if err != nil {
		t.Fatalf("Exporter: %v", err)
	}

	r, err := exp.Open(b)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	size, ok := exp.Size(b)
	if !ok || size == 0 {
		t.Fatalf("Size = (%d, %v), want the recorded size", size, ok)
	}

	buf := make([]byte, size)
	n, err := r.ReadAt(buf, 0)
	if err != nil && err != io.EOF {
		t.Fatalf("ReadAt: %v", err)
	}
	if int64(n) != size {
		t.Errorf("read %d bytes, want %d — the handle is not serving the whole file", n, size)
	}
	if !bytes.HasPrefix(buf, []byte("PK")) {
		t.Error("the bytes served are not a zip; the handle is not the book's epub")
	}
}
