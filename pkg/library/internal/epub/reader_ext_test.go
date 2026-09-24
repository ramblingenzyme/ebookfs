package epub_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/epubtest"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
)

// Cover is implemented in this package, not pkg/epub, so it has its own
// closed check.
func TestReaderClosedContract(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	r, err := epub.OpenReader(path, "OEBPS/cover.jpg")
	if err != nil {
		t.Fatal(err)
	}

	opf, err := r.OPF()
	if err != nil {
		t.Fatalf("OPF before close: %v", err)
	}
	if !bytes.Contains(opf, []byte("<dc:title")) {
		t.Errorf("OPF returned %d bytes without a title", len(opf))
	}
	if _, err := r.ReadAt(make([]byte, 4), 0); err != nil {
		t.Fatalf("ReadAt before close: %v", err)
	}
	if _, err := r.Cover(); err != nil {
		t.Fatalf("Cover before close: %v", err)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// library_ext_test.go runs this table against library.ErrClosed, and says
	// why both tests exist.
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"ReadAt", func() error { _, err := r.ReadAt(make([]byte, 4), 0); return err }},
		{"OPF", func() error { _, err := r.OPF(); return err }},
		{"Cover", func() error { _, err := r.Cover(); return err }},
		{"Close again", r.Close},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, epub.ErrClosed) {
				t.Errorf("err = %v, want ErrClosed", err)
			}
		})
	}
}

func TestReaderWithNoCover(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	r, err := epub.OpenReader(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if _, err := r.Cover(); err == nil {
		t.Error("Cover returned no error for an epub with no cover path")
	}
	if _, err := r.OPF(); err != nil {
		t.Errorf("OPF: %v", err)
	}
}
