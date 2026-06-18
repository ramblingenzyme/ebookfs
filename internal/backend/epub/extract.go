package epub

import (
	"archive/zip"
	"fmt"
	"io"
)

// ExtractCover returns the cover image bytes from the epub at epubPath.
// coverPath is the zip-relative path of the cover (from model.Bib.CoverPath).
func ExtractCover(epubPath, coverPath string) ([]byte, error) {
	r, err := zip.OpenReader(epubPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == coverPath {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}

	return nil, fmt.Errorf("cover not found in epub: %s", coverPath)
}
