package epub

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for image.DecodeConfig
	_ "image/png"  // register PNG decoder for image.DecodeConfig
	"path"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// Rewrite applies the requested changes from e to the epub at epubPath
// atomically. Every refusal check runs before any file is written — the
// original is never touched on error.
//
// It returns the book's authoritative Bib: the re-parsed result when a rewrite
// happened, or b.Bib unchanged when e carried no bib or cover edits (the
// no-op case). The result is a value, never a nil-pointer sentinel.
//
// b is used only for validation and for locating the cover entry within the
// zip; its EpubPath field is not read — the caller provides the resolved
// absolute path separately so the epub package does not need to know the store
// root.
func Rewrite(epubPath string, b *model.Book, e model.Edits) (model.Bib, error) {
	if !e.HasCoverEdit() && !e.HasBibEdits() {
		return b.Bib, nil
	}

	// Library.Edit is the enforcement point that validates every edit
	// (including the meta-only ones that noop out above); this re-check is a
	// defensive backstop so an unvalidated Edits can never rewrite an epub.
	if v := e.Validate(b); v != nil {
		return model.Bib{}, v
	}

	zrc, err := zip.OpenReader(epubPath)
	if err != nil {
		return model.Bib{}, err
	}
	defer zrc.Close()

	replace, err := createReplace(zrc, b, e)
	if err != nil {
		return model.Bib{}, err
	}

	bib, err := rewriteEpub(epubPath, zrc, replace)
	if err != nil {
		return model.Bib{}, err
	}
	return *bib, nil
}

func createReplace(zrc *zip.ReadCloser, b *model.Book, e model.Edits) (map[string][]byte, error) {
	enc, err := readEncryption(&zrc.Reader)
	if err != nil {
		return nil, err
	}
	replace := make(map[string][]byte, 2) // only 2 possible entries

	if e.HasCoverEdit() {
		want := coverFormat(b.CoverPath)
		if want == "" {
			return nil, fmt.Errorf("cover format not replaceable in place: %s", b.CoverPath)
		}
		_, got, err := image.DecodeConfig(bytes.NewReader(*e.Cover))
		if err != nil {
			return nil, fmt.Errorf("cover data is not a valid PNG or JPEG image: %w", err)
		}
		if got != want {
			return nil, fmt.Errorf("cover image is %s but the epub's cover entry %q is %s; a matching format is required (no transcoding)", got, b.CoverPath, want)
		}
		if findEntry(&zrc.Reader, b.CoverPath) == nil {
			return nil, fmt.Errorf("cover not found in epub: %s", b.CoverPath)
		}
		if enc.isEncrypted(b.CoverPath) {
			return nil, fmt.Errorf("refusing to replace encrypted cover: %s", b.CoverPath)
		}
		replace[b.CoverPath] = *e.Cover
	}

	if e.HasBibEdits() {
		opf, err := opfPath(&zrc.Reader)
		if err != nil {
			return nil, err
		}
		if enc.isEncrypted(opf) {
			return nil, fmt.Errorf("refusing to edit: package document %q is encrypted", opf)
		}
		opfBytes, err := readEntry(&zrc.Reader, opf)
		if err != nil {
			return nil, err
		}
		newOPF, err := editOPF(opfBytes, e)
		if err != nil {
			return nil, err
		}
		replace[opf] = newOPF
	}

	return replace, nil
}

// coverFormat maps a cover entry's path to the image format name (as reported by
// image.DecodeConfig) that may replace it in place, or "" if the extension is
// not an in-place-replaceable raster cover (matching calibre's png/jpg/jpeg
// restriction).
func coverFormat(coverPath string) string {
	switch strings.ToLower(path.Ext(coverPath)) {
	case ".jpg", ".jpeg":
		return "jpeg"
	case ".png":
		return "png"
	default:
		return ""
	}
}
