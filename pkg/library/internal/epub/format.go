package epub

import (
	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/formats"
	epubfile "github.com/ramblingenzyme/ebookfs/pkg/epub"
)

const mediaType = "application/epub+zip"

// epubFormat implements formats.Format for EPUB files.
type epubFormat struct{}

func init() {
	formats.Register(epubFormat{})
}

func (epubFormat) Format() book.Format       { return book.FormatEPUB }
func (epubFormat) Extension() string          { return ".epub" }
func (epubFormat) MediaType() string          { return mediaType }

func (epubFormat) Detect(path string) (bool, error) {
	f, err := epubfile.OpenFile(path)
	if err != nil {
		return false, nil
	}
	f.Close()
	return true, nil
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
	return OpenReader(path, coverPath)
}
