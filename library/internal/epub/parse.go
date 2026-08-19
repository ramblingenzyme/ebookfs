package epub

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/internal/epub/opf"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

const (
	containerPath = "META-INF/container.xml"
	metadataType  = "application/oebps-package+xml"
)

var (
	ErrNoRootfile      = errors.New("no package rootfile declared in container")
	ErrContainer       = errors.New("no container file found")
	ErrRootfileMissing = errors.New("declared package rootfile not found in archive")
	ErrNotEpub         = errors.New("not a valid epub")
)

// checkMimetype enforces the OCF requirement that the archive's "mimetype" entry
// declares the epub media type. Unlike calibre we reject rather than warn: a
// wrong mimetype usually means a non-epub zip, such as a mis-added .cbz.
func checkMimetype(filemap map[string]*zip.File) error {
	f := filemap[mimetypePath]
	if f == nil {
		return fmt.Errorf("%w: missing mimetype declaration", ErrNotEpub)
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	if got := strings.TrimSpace(string(data)); got != mimetypeValue {
		return fmt.Errorf("%w: unexpected mimetype %q", ErrNotEpub, got)
	}
	return nil
}

// getMetadataPath returns the package document's path from container.xml. exists
// reports whether a path is present in the container, and may be nil to skip the
// check.
//
// Some Kobo epubs declare several <rootfile> entries where only one exists in
// the zip, so missing ones are skipped and the first that exists is chosen.
func getMetadataPath(f *zip.File, exists func(string) bool) (string, error) {
	r, err := f.Open()
	if err != nil {
		return "", err
	}
	defer r.Close()

	var c container

	d := xml.NewDecoder(r)
	err = d.Decode(&c)
	if err != nil {
		return "", err
	}

	var declared string
	for _, rf := range c.Rootfiles {
		if rf.MediaType != metadataType {
			continue
		}
		if declared == "" {
			declared = rf.FullPath
		}
		if exists == nil || exists(rf.FullPath) {
			return rf.FullPath, nil
		}
	}

	if declared != "" {
		return "", ErrRootfileMissing
	}

	return "", ErrNoRootfile
}

func parsePackage(f *zip.File) (*opf.Doc, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()

	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return opf.Parse(b)
}

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

	filemap := make(map[string]*zip.File)
	for _, f := range r.File {
		filemap[f.Name] = f
	}

	if err := checkMimetype(filemap); err != nil {
		return nil, err
	}

	entry := filemap[containerPath]
	if entry == nil {
		return nil, ErrContainer
	}

	mpath, err := getMetadataPath(entry, func(name string) bool {
		_, ok := filemap[name]
		return ok
	})
	if err != nil {
		return nil, err
	}

	mfile := filemap[mpath]
	if mfile == nil {
		return nil, ErrRootfileMissing
	}

	doc, err := parsePackage(mfile)
	if err != nil {
		return nil, err
	}

	book, err := doc.Bib(path.Dir(mpath))
	if err != nil {
		return nil, err
	}

	// From the zip central directory, so nothing is decompressed. The epub's own
	// size is left to the library, which stats it for drift detection anyway.
	book.OpfSize = int64(mfile.UncompressedSize64)
	if book.CoverPath != "" {
		if cf := filemap[book.CoverPath]; cf != nil {
			book.CoverSize = int64(cf.UncompressedSize64)
		}
	}

	return book, nil
}
