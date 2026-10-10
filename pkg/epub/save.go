package epub

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // for image.DecodeConfig
	_ "image/png"  // for image.DecodeConfig
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

// ErrNoCover is returned by Cover and SetCover when the book has no cover
// image.
var ErrNoCover = errors.New("no cover in epub")

// SetCover replaces the cover image at the next Save. The new image is written
// over the existing cover entry, since the manifest keeps pointing at it. So
// the image must be in the same format, and a book with no cover cannot be
// given one.
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

// Save writes the fields that changed since Open, and any cover set by
// SetCover. If nothing changed, the file is not touched, and §5.5.5's
// dcterms:modified is not updated.
//
// The write is atomic. Save builds a temporary file next to the original,
// checks that it opens as an epub, then renames it over the original. If any
// step fails, the original is unchanged.
//
// After Save, the Book reads from the new file, and a later Save compares
// against the values just written.
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

// stage returns the entries Save must replace. Every refusal happens here,
// before anything is written.
func (b *Book) stage() (map[string][]byte, error) {
	// Refused whatever the edit touches; docs/DECISIONS.md #23 says why.
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

// moved records which fields changed since Open. write and changed both read
// it, so they always agree.
type moved struct {
	title, sortTitle, description, language, authors, series bool
	publisher, rights, subjects, contributors                bool
}

func (b *Book) moved() moved {
	o := b.orig
	return moved{
		title:        b.Title != o.Title,
		sortTitle:    b.SortTitle != o.SortTitle,
		description:  b.Description != o.Description,
		language:     b.Language != o.Language,
		authors:      !slices.Equal(b.Authors, o.Authors),
		series:       !sameSeries(b.Series, o.Series),
		publisher:    b.Publisher != o.Publisher,
		rights:       b.Rights != o.Rights,
		subjects:     !slices.Equal(b.Subjects, o.Subjects),
		contributors: !slices.Equal(b.Contributors, o.Contributors),
	}
}

// changed compares m to the zero value, so a field added to moved needs no
// change here.
func (m moved) changed() bool { return m != moved{} }

func sameSeries(a, c *Series) bool {
	if a == nil || c == nil {
		return a == c
	}
	return *a == *c
}

// write calls the package document's setters for the fields m names. It runs
// inside Apply, which compares the document before and after to decide whether
// to update dcterms:modified (§5.5.5).
func (b *Book) write(d *opf.Doc, m moved) {
	// opf.Doc.SetTitle says why the sort title is always passed, but the title
	// only when it changed.
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
	// Author and Series match opf's types field for field, so they convert
	// directly, and a field added to only one side stops this compiling.
	if m.authors {
		as := make([]opf.Author, len(b.Authors))
		for i, a := range b.Authors {
			as[i] = opf.Author(a)
		}
		d.SetAuthors(as)
	}
	if m.series {
		d.SetSeries((*opf.Series)(b.Series))
	}
	if m.publisher {
		d.SetPublisher(b.Publisher)
	}
	if m.rights {
		d.SetRights(b.Rights)
	}
	if m.subjects {
		d.SetSubjects(b.Subjects)
	}
	if m.contributors {
		cs := make([]opf.Contributor, len(b.Contributors))
		for i, c := range b.Contributors {
			cs[i] = opf.Contributor(c)
		}
		d.SetContributors(cs)
	}
}

// refitCoverPage updates the page that displays the cover to the new image's
// dimensions; package content says why. A candidate page counts only if it
// references the cover image, so a page that cannot be parsed is skipped.
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

// syncNCX keeps the NCX's title and authors in step; package ncx says why.
//
// An NCX that is encrypted or cannot be parsed is skipped rather than failing
// the edit. The package document is the authoritative metadata, and failing
// would leave a book with a broken NCX impossible to edit.
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
	// Logged, not returned: the edit succeeds, and this is the only notice that
	// the NCX is now out of date.
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

// rewrite follows calibre's safe_replace. It:
//   - writes mimetype first and copies it unchanged, so tools that sniff a
//     file's first bytes still recognise it;
//   - fails if a key in replace matches no entry, rather than silently
//     dropping the edit.
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
	defer zw.Close()

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

	// Opened before the original is touched, so a zip that will not read back
	// or a package document that will not parse fails here, and the original
	// survives.
	next, err := Open(tmpPath)
	if err != nil {
		return fmt.Errorf("rewritten epub failed validation: %w", err)
	}

	if err := os.Rename(tmpPath, b.path); err != nil {
		next.Close()
		return err
	}

	// The open handle follows the file through the rename, so next reads the
	// new file under the original path. b takes the handle over, so it is not
	// closed here.
	next.path = b.path
	b.File.Close()
	*b = *next
	return nil
}

// coverFormat allows only JPEG and PNG, as calibre does.
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
