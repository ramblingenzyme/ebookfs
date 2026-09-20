package kepub

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
)

// fakeSource returns a temp file filled with the given data.
type fakeSource struct {
	t   *testing.T
	dir string
}

func (s fakeSource) Content(_ int64) (epub.EpubReader, error) {
	path := filepath.Join(s.dir, "source.epub")
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &srcContent{f}, nil
}

// srcContent wraps an *os.File to satisfy epub.EpubReader. The kepub cache
// only reads from the reader; OPF and Cover are never called.
type srcContent struct {
	*os.File
}

func (c *srcContent) OPF() ([]byte, error)   { return nil, nil }
func (c *srcContent) Cover() ([]byte, error) { return nil, nil }

// newTestCache returns a cache over a source epub on disk whose conversions
// write body, so no test here reaches kepubify. Its Close is registered, which
// stops the warmer the cache started.
func newTestCache(t *testing.T, body string) (*Cache, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "source.epub"), []byte("epub-data"), 0644); err != nil {
		t.Fatal(err)
	}
	c := NewCache(dir, fakeSource{t: t, dir: dir})
	c.convertFn = func(_ context.Context, w io.Writer, _ io.ReaderAt, _ int64) error {
		_, err := w.Write([]byte(body))
		return err
	}
	t.Cleanup(func() { c.Close() })
	return c, dir
}

var makeBook = util.MakeMutableBook
