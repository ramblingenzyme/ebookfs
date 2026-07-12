package library

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/internal/kepub"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

type fakeEpubReader struct {
	*bytes.Reader
	closed bool
}

func (r *fakeEpubReader) Close() error {
	r.closed = true
	return nil
}

var _ EpubReader = (*fakeEpubReader)(nil)
var _ io.ReaderAt = (*fakeEpubReader)(nil)

type testLib struct {
	openEpubFn func(int64) (EpubReader, error)
}

func (l testLib) Close() error                                     { return nil }
func (l testLib) Exporter(_ config.ReaderConfig) (Exporter, error) { return nil, nil }
func (l testLib) CreateIngest() (IngestHandle, error)              { return nil, nil }
func (l testLib) Query(_ model.Filter) ([]*model.Book, error)      { return nil, nil }
func (l testLib) Stats() (*model.Stats, error)                     { return nil, nil }
func (l testLib) Reindex() error                                   { return nil }
func (l testLib) OpenEpub(id int64) (EpubReader, error) {
	if l.openEpubFn != nil {
		return l.openEpubFn(id)
	}
	return nil, nil
}
func (l testLib) ExtractCover(id int64) ([]byte, error) { return nil, nil }
func (l testLib) ExtractOPF(id int64) ([]byte, error)   { return nil, nil }
func (l testLib) Edit(id int64, e model.Edits) (*model.Book, error) {
	return nil, nil
}
func (l testLib) Delete(id int64) error { return nil }

var makeBook = testutil.MakeBook

func TestEpubExporter_Open(t *testing.T) {
	lib := testLib{
		openEpubFn: func(_ int64) (EpubReader, error) {
			return &fakeEpubReader{Reader: bytes.NewReader([]byte("data"))}, nil
		},
	}
	exp := epubExporter{lib: lib}
	book := makeBook(1, "Test", "Author")

	r, err := exp.Open(book)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if r == nil {
		t.Fatal("Open returned nil reader")
	}
	r.Close()
}

func TestEpubExporter_Size_Success(t *testing.T) {
	content := []byte("hello epub")
	path := t.TempDir() + "/test.epub"
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	book := makeBook(1, "Test", "Author")
	book.EpubPath = path
	book.EpubSize = int64(len(content))

	exp := epubExporter{lib: testLib{}}
	size, ok := exp.Size(book)
	if !ok {
		t.Fatal("Size should return ok=true for a valid file")
	}
	if size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", size, len(content))
	}
}

func TestEpubExporter_Size_MissingFile(t *testing.T) {
	book := makeBook(1, "Test", "Author")
	book.EpubPath = "/nonexistent/missing.epub"

	exp := epubExporter{lib: testLib{}}
	size, ok := exp.Size(book)
	if ok {
		t.Error("Size should return ok=false for a missing file")
	}
	if size != 0 {
		t.Errorf("Size = %d, want 0", size)
	}
}

func TestEpubExporter_Filename(t *testing.T) {
	exp := epubExporter{lib: testLib{}}
	book := makeBook(1, "Test", "Author")
	book.EpubFilename = "mybook.epub"

	name := exp.Filename(book)
	if name != "mybook.epub" {
		t.Errorf("Filename = %q, want %q", name, "mybook.epub")
	}
}

func TestEpubExporter_Warm(t *testing.T) {
	exp := epubExporter{}
	exp.Warm(nil) // no-op, must not panic
}

func TestEpubExporter_Includes(t *testing.T) {
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
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp := epubExporter{statuses: tt.statuses}
			book := makeBook(1, "Test", "Author")
			book.Meta.Status = tt.status
			if got := exp.Includes(book); got != tt.want {
				t.Errorf("Includes = %v, want %v", got, tt.want)
			}
		})
	}
}

type dummyKepubSource struct{}

func (dummyKepubSource) OpenEpub(int64) (model.EpubReader, error) {
	return nil, errors.New("dummy source: no epub")
}

func TestKepubCacheDelegates(t *testing.T) {
	dir := t.TempDir()
	kc := &kepubCache{
		statuses: []string{"reading"},
		c:        kepub.NewCache(dir, dummyKepubSource{}),
	}

	b := makeBook(1, "Test", "Alice")
	b.EpubFilename = "mybook.epub"

	// statuses has the configured slice.
	if s := kc.statuses; len(s) != 1 || s[0] != "reading" {
		t.Errorf("Statuses = %v, want [reading]", s)
	}

	// Filename delegates to k.c.Filename.
	if fn := kc.Filename(b); fn != "mybook.kepub.epub" {
		t.Errorf("Filename = %q, want %q", fn, "mybook.kepub.epub")
	}

	// Dirname delegates to exportDirname.
	if dn := kc.Dirname(b); dn != "Alice" {
		t.Errorf("Dirname = %q, want %q", dn, "Alice")
	}

	// Size reports cold when no cache file exists.
	_, ok := kc.Size(b)
	if ok {
		t.Error("Size should report cold for missing cache file")
	}

	// Warm must not panic.
	kc.Warm(b)

	// Open returns an error because the dummy source returns nil.
	_, err := kc.Open(b)
	if err == nil {
		t.Error("expected error from Open with dummy source")
	}

	// Close stops the warmer without error.
	if err := kc.close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

func TestEpubExporter_Dirname(t *testing.T) {
	tests := []struct {
		name     string
		authorFn func() []model.Author
		want     string
	}{
		{"single author", func() []model.Author { return []model.Author{{Name: "Alice"}} }, "Alice"},
		{"two authors", func() []model.Author { return []model.Author{{Name: "Alice"}, {Name: "Bob"}} }, "Alice & Bob"},
		{"multiple authors", func() []model.Author { return []model.Author{{Name: "Alice"}, {Name: "Bob"}, {Name: "Carol"}} }, "Alice & Bob & Carol"},
		{"empty author name", func() []model.Author { return []model.Author{{Name: ""}} }, model.UnknownAuthor},
		{"mixed empty and valid", func() []model.Author { return []model.Author{{Name: ""}, {Name: "Alice"}} }, "Alice"},
		{"no authors", func() []model.Author { return nil }, model.UnknownAuthor},
		{"colon in name", func() []model.Author { return []model.Author{{Name: "Title: Sub"}} }, "Title- Sub"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp := epubExporter{}
			authors := tt.authorFn()
			names := make([]string, len(authors))
			for i, a := range authors {
				names[i] = a.Name
			}
			b := makeBook(1, "Test", names...)
			b.Authors = authors
			got := exp.Dirname(b)
			if got != tt.want {
				t.Errorf("Dirname = %q, want %q", got, tt.want)
			}
		})
	}
}
