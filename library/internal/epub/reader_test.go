package epub_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
)

// Reader documents a closed contract in three places — the closed field, Close's
// "safe to call multiple times", and on each accessor.
// The type satisfies model.EpubReader and is reached from the 9P read path
// through vfile.ReadAtFile, where a client holding a fid across a re-ingest is
// exactly how a use-after-close arises.
func TestReaderClosedContract(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))
	r, err := epub.OpenReader(path, "OEBPS/cover.jpg")
	if err != nil {
		t.Fatal(err)
	}

	// Working before the close, so the errors after it mean something.
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

// A reader opened for a book with no cover reports that rather than returning
// empty bytes: Bib.CoverPath is "" when the epub carries no cover image, and
// that value is handed straight to OpenReader.
func TestReaderWithNoCover(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))
	r, err := epub.OpenReader(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if _, err := r.Cover(); err == nil {
		t.Error("Cover returned no error for an epub with no cover path")
	}
	// The rest of the reader still works — no cover is not a broken reader.
	if _, err := r.OPF(); err != nil {
		t.Errorf("OPF: %v", err)
	}
}
