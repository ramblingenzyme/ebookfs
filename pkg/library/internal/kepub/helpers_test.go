package kepub

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
)

// fakeSidecarPath implements the path function for tests. It returns paths
// under dir/{id}/.sidecar/kepub.epub, creating the .sidecar directory on demand.
func fakeSidecarPath(dir string) func(int64, string) (string, error) {
	return func(id int64, name string) (string, error) {
		cacheDir := filepath.Join(dir, fmt.Sprintf("%d", id), ".sidecar")
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			return "", err
		}
		return filepath.Join(cacheDir, name), nil
	}
}

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

// The kepub cache only reads; OPF and Cover are never called.
type srcContent struct {
	*os.File
}

func (c *srcContent) OPF() ([]byte, error)   { return nil, nil }
func (c *srcContent) Cover() ([]byte, error) { return nil, nil }

// newTestCache writes body instead of converting, so no test here reaches
// kepubify. The registered Close stops the warmer the cache started.
func newTestCache(t *testing.T, body string) (*Cache, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "source.epub"), []byte("epub-data"), 0644); err != nil {
		t.Fatal(err)
	}
	c := NewCache(fakeSidecarPath(dir), fakeSource{t: t, dir: dir})
	c.convertFn = func(_ context.Context, w io.Writer, _ io.ReaderAt, _ int64) error {
		_, err := w.Write([]byte(body))
		return err
	}
	t.Cleanup(func() { c.Close() })
	return c, dir
}

// The cache takes an immutable snapshot, so a test sets its fields on the
// mutable book and wraps it at the handoff, as the views tests do.
var (
	makeBook = util.MakeMutableBook
	wrapBook = util.WrapBook
)
