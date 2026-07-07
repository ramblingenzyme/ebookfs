package epub

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
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
// declares the epub media type. ebookfs's parser is deliberately strict — it
// already rejects epubs with no title or author — so a missing or wrong mimetype
// (the signature of a non-epub zip such as a mis-added .cbz) is rejected here
// with a clear error, rather than warned about and carried forward the way
// calibre does.
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
// reports whether a given path is present in the container; it may be nil to
// skip the check.
//
// Some Kobo epubs declare multiple <rootfile> entries where only one actually
// exists in the zip. Mirroring calibre, the first package rootfile that exists
// is chosen and missing ones are skipped. If a package rootfile is declared but
// none of them exist, ErrRootfileMissing is returned (distinct from ErrNoRootfile,
// which means no package rootfile was declared at all).
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

func parsePackage(f *zip.File) (*opfPackage, error) {
	r, err := f.Open()
	if err != nil {
		return &opfPackage{}, err
	}
	defer r.Close()

	var pkg opfPackage

	d := xml.NewDecoder(r)
	err = d.Decode(&pkg)

	return &pkg, err
}

func Parse(bpath string) (*Book, error) {
	// zip.OpenReader opens the file and validates the zip structure: a missing or
	// unreadable path surfaces its os error verbatim, while a non-zip file is
	// reported as zip.ErrFormat, which we translate into ErrNotEpub.
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

	pkg, err := parsePackage(mfile)
	if err != nil {
		return nil, err
	}
	pkg.BasePath = path.Dir(mpath)

	book, err := translate(pkg)
	if err != nil {
		return nil, err
	}

	// Capture OPF, cover, and epub sizes. OPF and cover sizes come
	// from the zip central directory without decompressing the entries; epub
	// size requires a cheap os.Stat. All three are carried on epub.Book and
	// propagated to model.Bib via bibFromEpub.
	book.OpfSize = int64(mfile.UncompressedSize64)
	if book.CoverPath != "" {
		if cf := filemap[book.CoverPath]; cf != nil {
			book.CoverSize = int64(cf.UncompressedSize64)
		}
	}
	if fi, err := os.Stat(bpath); err == nil {
		book.EpubSize = fi.Size()
	}

	return book, nil
}
