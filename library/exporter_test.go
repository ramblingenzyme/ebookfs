package library

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/config"
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
	openEpubFn func(*model.Book) (EpubReader, error)
}

func (l testLib) Close() error                                     { return nil }
func (l testLib) Exporter(_ config.ReaderConfig) (Exporter, error) { return nil, nil }
func (l testLib) CreateIngest() (*IngestHandle, error)             { return nil, nil }
func (l testLib) Query(_ model.Filter) ([]*model.Book, error)      { return nil, nil }
func (l testLib) Reindex() error                                   { return nil }
func (l testLib) OpenEpub(b *model.Book) (EpubReader, error) {
	if l.openEpubFn != nil {
		return l.openEpubFn(b)
	}
	return nil, nil
}
func (l testLib) ExtractCover(b *model.Book) ([]byte, error) { return nil, nil }
func (l testLib) ExtractOPF(b *model.Book) ([]byte, error)   { return nil, nil }
func (l testLib) Edit(id int64, e model.Edits) (*model.Book, error) {
	return nil, nil
}
func (l testLib) WriteCover(id int64, img []byte) error { return nil }
func (l testLib) Delete(id int64) error                 { return nil }

func makeBook(id int64, title string, authors ...string) *model.Book {
	auths := make([]model.Author, len(authors))
	for i, name := range authors {
		auths[i] = model.Author{Name: name}
	}
	return model.NewBook(
		model.Bib{Title: title, Authors: auths},
		model.Meta{ID: id},
		model.Location{},
	)
}

func TestEpubExporter_Open(t *testing.T) {
	lib := testLib{
		openEpubFn: func(b *model.Book) (EpubReader, error) {
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
