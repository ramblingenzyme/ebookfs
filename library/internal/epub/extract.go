package epub

import (
	"archive/zip"
)

// ExtractCover returns the cover image bytes from the epub at epubPath.
// coverPath is the zip-relative path of the cover (from model.Bib.CoverPath).
func ExtractCover(epubPath, coverPath string) ([]byte, error) {
	r, err := zip.OpenReader(epubPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return readEntry(&r.Reader, coverPath)
}

// ExtractOPF returns the raw OPF XML bytes from the epub at epubPath.
func ExtractOPF(epubPath string) ([]byte, error) {
	r, err := zip.OpenReader(epubPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	path, err := opfPath(&r.Reader)
	if err != nil {
		return nil, err
	}
	return readEntry(&r.Reader, path)
}
