package epub

import (
	"errors"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	epubfile "github.com/ramblingenzyme/ebookfs/pkg/epub"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/formats"
)

// epubFormat implements formats.Format for EPUB files.
type epubFormat struct{}

// NewFormat returns the EPUB format implementation.
func NewFormat() formats.Format { return epubFormat{} }

func (epubFormat) Format() book.Format { return book.FormatEPUB }

func (epubFormat) Detect(path string) (bool, error) {
	f, err := epubfile.OpenFile(path)
	switch {
	case err == nil:
		f.Close()
		return true, nil
	case errors.Is(err, epubfile.ErrNotEpub):
		return false, nil
	case formats.IsIO(err):
		return false, err
	default:
		return true, nil
	}
}

func (epubFormat) Parse(path string) (*book.Bib, error) {
	return Parse(path)
}

func (epubFormat) Rewrite(path string, current *book.Book, e book.Edits) (*book.Bib, error) {
	bib, err := Rewrite(path, current, e)
	if err != nil {
		return nil, err
	}
	return &bib, nil
}

func (epubFormat) OpenReader(path, coverPath string) (formats.Reader, error) {
	r, err := OpenReader(path, coverPath)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (epubFormat) TempPattern() string { return epubfile.SaveTempPattern }
