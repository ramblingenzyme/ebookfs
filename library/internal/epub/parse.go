package epub

import (
	"archive/zip"
	"errors"
	"fmt"
	"path"

	"github.com/ramblingenzyme/ebookfs/library/internal/epub/opf"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

func Parse(bpath string) (*model.Bib, error) {
	// A missing or unreadable path surfaces its os error verbatim; a non-zip file
	// comes back as zip.ErrFormat, which becomes ErrNotEpub.
	r, err := zip.OpenReader(bpath)
	if err != nil {
		if errors.Is(err, zip.ErrFormat) {
			return nil, fmt.Errorf("%w: %s: %w", ErrNotEpub, bpath, err)
		}
		return nil, err
	}
	defer r.Close()

	a, err := openArchive(&r.Reader)
	if err != nil {
		return nil, err
	}
	if err := a.validate(); err != nil {
		return nil, err
	}

	opfBytes, err := a.read(a.opf)
	if err != nil {
		return nil, err
	}
	doc, err := opf.Parse(opfBytes)
	if err != nil {
		return nil, err
	}

	book, err := doc.Bib(path.Dir(a.opf))
	if err != nil {
		return nil, err
	}

	// From the zip central directory, so nothing is decompressed. The epub's own
	// size is left to the library, which stats it for drift detection anyway.
	book.OpfSize = a.size(a.opf)
	if book.CoverPath != "" {
		book.CoverSize = a.size(book.CoverPath)
	}

	return book, nil
}
