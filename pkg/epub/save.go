package epub

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for image.DecodeConfig
	_ "image/png"  // register PNG decoder for image.DecodeConfig
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/content"
	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/ncx"
	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/ocf"
	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/opf"
)

// ErrNoCover is returned by Cover when the package document points at no cover
// image, and by SetCover when there is none to replace.
var ErrNoCover = errors.New("no cover in epub")

// SetCover stages a replacement cover image, written by the next Save. The
// image replaces the entry the manifest already names, in place and in the same
// format, so the manifest, the cover-image property and the legacy
// <meta name="cover"> keep pointing at what they already did — which is why
// there is no transcoding and no way to add a cover to a book without one.
//
// Staged rather than written, so that a cover change and a metadata change
// rebuild the archive once between them.
func (b *Book) SetCover(img []byte) error {
	if b.coverPath == "" {
		return ErrNoCover
	}
	want := coverFormat(b.coverPath)
	if want == "" {
		return fmt.Errorf("cover format not replaceable in place: %s", b.coverPath)
	}
	_, got, err := image.DecodeConfig(bytes.NewReader(img))
	if err != nil {
		return fmt.Errorf("cover data is not a valid PNG or JPEG image: %w", err)
	}
	if got != want {
		return fmt.Errorf("cover image is %s but the epub's cover entry %q is %s; a matching format is required (no transcoding)", got, b.coverPath, want)
	}
	if !b.has(b.coverPath) {
		return fmt.Errorf("cover not found in epub: %s", b.coverPath)
	}
	b.cover = img
	return nil
}

// Save writes every field that differs from what Open read, and the cover
// SetCover staged, back to the file. A field left as it was parsed is not
// written, so a Save that changes nothing touches nothing: the archive is not
// rebuilt, and §5.5.5's dcterms:modified is not stamped.
//
// The write is atomic. A temp file beside the original is built with the
// changed entries swapped and everything else copied byte for byte, re-opened
// to prove it is still an epub, and only then renamed over the original — so a
// failure at any point leaves the original exactly as it was.
//
// After a successful Save the Book reads from the rewritten file, and its
// fields become the new baseline.
func (b *Book) Save() error {
	if b.File == nil || b.doc == nil {
		return errors.New("epub: Save on a Book that was not opened")
	}
	replace, err := b.stage()
	if err != nil {
		return err
	}
	if len(replace) == 0 {
		return nil
	}
	return b.rewrite(replace)
}

// stage collects the entries Save must replace, applying every refusal before
// anything is written.
func (b *Book) stage() (map[string][]byte, error) {
	// Before any other refusal: it does not depend on which entries the edit
	// turns out to touch. DECISIONS.md #23 says why it is not narrowed to them.
	if b.has(ocf.SignaturesPath) {
		return nil, fmt.Errorf("refusing to edit: the epub is signed (%s) and an edit would invalidate the signature", ocf.SignaturesPath)
	}

	enc, err := b.a.readEncryption()
	if err != nil {
		return nil, err
	}

	replace := make(map[string][]byte, 3)

	if b.cover != nil {
		if enc.IsEncrypted(b.coverPath) {
			return nil, fmt.Errorf("refusing to replace encrypted cover: %s", b.coverPath)
		}
		replace[b.coverPath] = b.cover
		cfg, _, err := image.DecodeConfig(bytes.NewReader(b.cover))
		if err != nil {
			return nil, err
		}
		if err := b.refitCoverPage(enc, cfg.Width, cfg.Height, replace); err != nil {
			return nil, err
		}
	}

	if m := b.moved(); m.changed() {
		if enc.IsEncrypted(b.PackagePath()) {
			return nil, fmt.Errorf("refusing to edit: package document %q is encrypted", b.PackagePath())
		}
		// A field assigned the value the document already carries leaves
		// nothing to replace, which is what lets Save skip the rebuild.
		if b.doc.Apply(func(d *opf.Doc) { b.write(d, m) }) {
			out, err := b.doc.Bytes()
			if err != nil {
				return nil, err
			}
			replace[b.PackagePath()] = out
		}
		if err := b.syncNCX(enc, m, replace); err != nil {
			return nil, err
		}
	}

	return replace, nil
}

// moved is which fields differ from what Open read: the one answer to that
// question, so a field added to Book cannot be written by one half of Save and
// ignored by the other.
type moved struct {
	title, sortTitle, description, language, authors, series bool
}

func (b *Book) moved() moved {
	o := b.orig
	return moved{
		title:       b.Title != o.Title,
		sortTitle:   b.SortTitle != o.SortTitle,
		description: b.Description != o.Description,
		language:    b.Language != o.Language,
		authors:     !slices.Equal(b.Authors, o.Authors),
		series:      !sameSeries(b.Series, o.Series),
	}
}

func (m moved) changed() bool {
	return m.title || m.sortTitle || m.description || m.language || m.authors || m.series
}

func sameSeries(a, c *Series) bool {
	if a == nil || c == nil {
		return a == c
	}
	return *a == *c
}

// write drives the package document's setters for the fields m names. It is
// handed to Apply rather than called directly so that the §5.5.5 byte compare
// brackets every write.
func (b *Book) write(d *opf.Doc, m moved) {
	// The title's two halves are not independent: writing a title takes the
	// document's other dc:title segments with it, which a sort-title edit must
	// not do. So the title is passed only when it moved, while the sort title
	// is restated whenever either did — restating it keeps a title-only change
	// from dropping the sort title the book carried.
	if m.title || m.sortTitle {
		var title *string
		if m.title {
			title = &b.Title
		}
		d.SetTitle(title, &b.SortTitle)
	}
	if m.description {
		d.SetDescription(b.Description)
	}
	if m.language {
		d.SetLanguage(b.Language)
	}
	if m.authors {
		as := make([]opf.Author, len(b.Authors))
		for i, a := range b.Authors {
			as[i] = opf.Author{Name: a.Name, SortName: a.SortName}
		}
		d.SetAuthors(as)
	}
	if m.series {
		var s *opf.Series
		if b.Series != nil {
			s = &opf.Series{Name: b.Series.Name, Index: b.Series.Index}
		}
		d.SetSeries(s)
	}
}

// refitCoverPage rewrites the page displaying the cover to the new image's
// dimensions; package content says why that is needed. A candidate from the
// package document is confirmed by finding the cover image inside it, so an
// unreadable one is skipped silently — it was never confirmed to be the cover
// page.
func (b *Book) refitCoverPage(enc *ocf.EncryptionInfo, width, height int, replace map[string][]byte) error {
	for _, entry := range b.doc.CoverPages(path.Dir(b.PackagePath())) {
		if !b.has(entry) || enc.IsEncrypted(entry) {
			continue
		}
		data, err := b.ReadEntry(entry)
		if err != nil {
			return err
		}
		doc, err := content.Parse(data, entry)
		if err != nil {
			continue
		}
		if doc.FitCover(b.coverPath, width, height) {
			out, err := doc.Bytes()
			if err != nil {
				return err
			}
			replace[entry] = out
			return nil
		}
	}
	return nil
}

// syncNCX adds the rewritten NCX to replace, when the package declares one and
// the edit touched a field it carries; package ncx says why.
//
// An NCX that cannot be read — encrypted, or malformed — is skipped rather than
// failing the edit. The package document is the metadata of record, and
// refusing would leave a book that arrived with an unreadable NCX permanently
// unrenameable.
func (b *Book) syncNCX(enc *ocf.EncryptionInfo, m moved, replace map[string][]byte) error {
	if !m.title && !m.authors {
		return nil
	}
	entry := b.doc.NCXPath(path.Dir(b.PackagePath()))
	if entry == "" || !b.has(entry) || enc.IsEncrypted(entry) {
		return nil
	}

	data, err := b.ReadEntry(entry)
	if err != nil {
		return err
	}
	// Logged rather than returned: the edit succeeds, and nothing else reports
	// that half of what the book says about itself is now stale.
	doc, err := ncx.Parse(data)
	if err != nil {
		slog.Warn("epub: skipping unreadable NCX; its title and authors will not match the package document",
			"entry", entry, "error", err)
		return nil
	}

	var title *string
	if m.title {
		title = &b.Title
	}
	var names []string
	if m.authors {
		names = make([]string, len(b.Authors))
		for i, a := range b.Authors {
			names[i] = a.Name
		}
	}
	if doc.Apply(title, names) {
		out, err := doc.Bytes()
		if err != nil {
			return err
		}
		replace[entry] = out
	}
	return nil
}

// rewrite writes a temp epub beside the original with the named entries
// swapped, proves it opens, then renames it over the original and adopts it.
// The temp file is cleaned up on any failure.
//
// Faithfulness rules, matching what calibre's safe_replace honours:
//   - mimetype is written first and copied byte-for-byte, keeping its STORED
//     form so magic-byte sniffers still recognise the file;
//   - untouched entries are copied raw, preserving order, modtime and method;
//   - every key in replace must match an entry, so a mistargeted edit fails
//     loudly rather than silently dropping.
func (b *Book) rewrite(replace map[string][]byte) error {
	dir := filepath.Dir(b.path)
	tmp, err := os.CreateTemp(dir, ".ebookfs-*.epub.tmp")
	if err != nil {
		return err
	}

	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	defer tmp.Close()

	zw := zip.NewWriter(tmp)
	if err := b.a.writeTo(zw, replace); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	// Opened before the original is touched, so structural breakage — a zip we
	// cannot read back, a package document we cannot parse — fails here and the
	// original survives.
	next, err := Open(tmpPath)
	if err != nil {
		return fmt.Errorf("rewritten epub failed validation: %w", err)
	}
	if err := os.Rename(tmpPath, b.path); err != nil {
		next.Close()
		return err
	}

	// The handle survives the rename, since it holds the inode rather than the
	// name, so next reads the rewritten bytes under the original's path.
	next.path = b.path
	b.File.Close()
	*b = *next
	return nil
}

// coverFormat maps a cover entry's path to the image.DecodeConfig format name
// that may replace it in place, or "" for anything outside calibre's png/jpg/jpeg
// restriction.
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
