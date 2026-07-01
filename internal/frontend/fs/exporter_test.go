package fs

import (
	"bytes"
	"os"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/backend/library"
	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

func TestEpubExporter_Open(t *testing.T) {
	lib := fakeLib{
		openEpubFn: func(b *model.Book) (library.EpubReader, error) {
			return &fakeEpubReader{Reader: bytes.NewReader([]byte("data"))}, nil
		},
	}
	exp := NewEpubExporter(lib)
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

	exp := NewEpubExporter(fakeLib{})
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

	exp := NewEpubExporter(fakeLib{})
	size, ok := exp.Size(book)
	if ok {
		t.Error("Size should return ok=false for a missing file")
	}
	if size != 0 {
		t.Errorf("Size = %d, want 0", size)
	}
}

func TestEpubExporter_Ensure(t *testing.T) {
	exp := NewEpubExporter(fakeLib{})
	err := exp.Ensure(makeBook(1, "Test", "Author"))
	if err != nil {
		t.Errorf("Ensure should always return nil, got %v", err)
	}
}

func TestEpubExporter_Filename(t *testing.T) {
	exp := NewEpubExporter(fakeLib{})
	book := makeBook(1, "Test", "Author")
	book.EpubFilename = "mybook.epub"

	name := exp.Filename(book)
	if name != "mybook.epub" {
		t.Errorf("Filename = %q, want %q", name, "mybook.epub")
	}
}
