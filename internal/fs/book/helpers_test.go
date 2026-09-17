// The doubles for the two library interfaces this package declares, and for the
// reader they both hand back. They live here rather than in a shared package so
// each one is exactly what these tests use: nothing here stubs OPF, and nothing
// stubs the four exporter methods that belong to the view rather than the file.

package book

import (
	"bytes"
	"errors"

	"github.com/ramblingenzyme/ebookfs/library"
)

// contentReader is a ContentReader. A nil hook errors so a test that reaches the
// read path without stubbing it fails loudly rather than reading nothing.
type contentReader struct {
	ContentFn func(int64) (library.EpubReader, error)
}

func (c contentReader) Content(id int64) (library.EpubReader, error) {
	if c.ContentFn != nil {
		return c.ContentFn(id)
	}
	return nil, errors.New("contentReader: no ContentFn")
}

var _ ContentReader = contentReader{}

// renderer is a Renderer. Open errors without a hook for the same reason Content
// does. Size reports cold, which is what the real exporter answers before a
// rendition is cached.
type renderer struct {
	OpenFn func(*library.Book) (library.EpubReader, error)
	SizeFn func(*library.Book) (int64, bool)
}

func (e renderer) Open(b *library.Book) (library.EpubReader, error) {
	if e.OpenFn != nil {
		return e.OpenFn(b)
	}
	return nil, errors.New("renderer: no OpenFn")
}

func (e renderer) Size(b *library.Book) (int64, bool) {
	if e.SizeFn != nil {
		return e.SizeFn(b)
	}
	return 0, false
}

var _ Renderer = renderer{}

// epubReader is a library.EpubReader over a fixed buffer, with the cover
// extraction result injected. No test here reads the OPF through a double, so
// OPF is a stub.
type epubReader struct {
	*bytes.Reader
	CoverFn func() ([]byte, error)
}

func (r *epubReader) Close() error         { return nil }
func (r *epubReader) OPF() ([]byte, error) { return nil, nil }

func (r *epubReader) Cover() ([]byte, error) {
	if r.CoverFn != nil {
		return r.CoverFn()
	}
	return nil, nil
}

var _ library.EpubReader = (*epubReader)(nil)
