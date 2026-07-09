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

// Prepare creates a temporary epub with the requested changes from e applied
// to the epub at b.EpubPath. Every refusal check runs before the temp file is
// written — the original is never touched on error. The returned Commit can be
// applied atomically via Commit() or discarded via Discard().
func Prepare(b *model.Book, e model.Edits) (*Commit, error) {
	if !e.HasCoverEdit() && !e.HasBibEdits() {
		return &Commit{noop: true}, nil
	}

	if v := e.Validate(b); v != nil {
		return nil, v
	}

	replace := make(map[string][]byte)

	zrc, err := zip.OpenReader(b.EpubPath)
	if err != nil {
		return nil, err
	}
	defer zrc.Close()

	enc, err := readEncryption(&zrc.Reader)
	if err != nil {
		return nil, err
	}

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

	return prepareEpub(b.EpubPath, replace)
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
