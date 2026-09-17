package library_test

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/library"
)

// exporterFor builds an Exporter the way main.go does, through lib.Exporter,
// rather than by constructing epubExporter or kepubCache directly. convert
// picks the kepub rendition, which requires a cache dir outside the library
// root. Which concrete type comes back is the library's choice, and these
// tests only ever see the interface.
func exporterFor(t *testing.T, convert bool, statuses ...string) library.Exporter {
	t.Helper()
	lib := openTestLibrary(t)
	cfg := library.ReaderConfig{Statuses: statuses}
	if convert {
		cfg.Convert, cfg.CacheDir = true, t.TempDir()
	}
	exp, err := lib.Exporter(cfg)
	if err != nil {
		t.Fatalf("Exporter: %v", err)
	}
	return exp
}

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

// The status filter runs over both renditions. They carry separate copies of the
// same one-line rule, and it decides what a reader mount can see, so a
// divergence between them is a mount quietly serving the wrong set of books.
func TestExporterIncludes(t *testing.T) {
	tests := []struct {
		name     string
		statuses []string
		status   string
		want     bool
	}{
		{"matching status", []string{"unread", "reading"}, "unread", true},
		{"non-matching status", []string{"unread", "reading"}, "archived", false},
		{"empty statuses", nil, "unread", false},
		{"empty book status", []string{"unread"}, "", false},
		{"empty book status with empty in statuses", []string{""}, "", true},
	}
	for kind, convert := range map[string]bool{"epub": false, "kepub": true} {
		t.Run(kind, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					b := makeBook(1, "Test", "Author")
					b.Meta.Status = tt.status
					exp := exporterFor(t, convert, tt.statuses...)
					if got := exp.Includes(testutil.WrapBook(b)); got != tt.want {
						t.Errorf("Includes = %v, want %v", got, tt.want)
					}
				})
			}
		})
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

// Size answers from the size recorded at index time rather than the filesystem:
// the book's path points at nothing, and the call must still succeed without
// touching disk. Every indexed book was stat'd on the way in, so a missing file
// surfaces at Open rather than as a length the exporter has to guess at.
func TestEpubExporter_Size_ReportsRecordedSize(t *testing.T) {
	b := makeBook(1, "Test", "Author")
	b.EpubPath = "/nonexistent/missing.epub"
	b.EpubSize = 4242

	size, ok := exporterFor(t, false).Size(testutil.WrapBook(b))
	if !ok {
		t.Error("Size should be known for any indexed book")
	}
	if size != 4242 {
		t.Errorf("Size = %d, want 4242", size)
	}
}

// The other side of that guard. A book carrying no recorded size was never
// observed, and reporting 0 as authoritative would have 9P advertise a
// zero-length file and export sizing believe it, so the size reads as unknown
// and the caller falls back rather than trusting it.
func TestEpubExporter_Size_Unrecorded(t *testing.T) {
	b := makeBook(1, "Test", "Author") // EpubSize left at its zero value

	size, ok := exporterFor(t, false).Size(testutil.WrapBook(b))
	if ok {
		t.Errorf("Size = (%d, true) for a book with no recorded size, want it reported as unknown", size)
	}
}

func TestEpubExporter_Filename(t *testing.T) {
	b := makeBook(1, "Test", "Author")
	b.EpubPath = "mybook.epub"

	name := exporterFor(t, false).Filename(testutil.WrapBook(b))
	if name != "mybook.epub" {
		t.Errorf("Filename = %q, want %q", name, "mybook.epub")
	}
}

func TestEpubExporter_Warm(t *testing.T) {
	exporterFor(t, false).Warm(nil)
}

func TestEpubExporter_Dirname(t *testing.T) {
	tests := []struct {
		name     string
		authorFn func() []library.Author
		want     string
	}{
		{"single author", func() []library.Author { return []library.Author{{Name: "Alice"}} }, "Alice"},
		{"two authors", func() []library.Author { return []library.Author{{Name: "Alice"}, {Name: "Bob"}} }, "Alice & Bob"},
		{"multiple authors", func() []library.Author {
			return []library.Author{{Name: "Alice"}, {Name: "Bob"}, {Name: "Carol"}}
		}, "Alice & Bob & Carol"},
		{"empty author name", func() []library.Author { return []library.Author{{Name: ""}} }, book.UnknownAuthor},
		{"mixed empty and valid", func() []library.Author { return []library.Author{{Name: ""}, {Name: "Alice"}} }, "Alice"},
		{"no authors", func() []library.Author { return nil }, book.UnknownAuthor},
		{"colon in name", func() []library.Author { return []library.Author{{Name: "Title: Sub"}} }, "Title- Sub"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authors := tt.authorFn()
			names := make([]string, len(authors))
			for i, a := range authors {
				names[i] = a.Name
			}
			b := makeBook(1, "Test", names...)
			b.Authors = authors
			got := exporterFor(t, false).Dirname(testutil.WrapBook(b))
			if got != tt.want {
				t.Errorf("Dirname = %q, want %q", got, tt.want)
			}
		})
	}
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
