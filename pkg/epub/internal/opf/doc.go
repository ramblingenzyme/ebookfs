// Package opf reads and writes the metadata in an EPUB package document. The
// zip around it belongs to package epub, and the XML beneath it to pkgdoc.
//
// Each field reads and writes through one type, so the two cannot disagree,
// and writing back what was read adds nothing the file did not carry. Nothing
// is defaulted or rejected; Doc.Metadata says what that leaves to the caller.
//
//   - A field with an encoding of its own is a type with get and set (title,
//     authors, series, modified). Description and language are one plain
//     element each, read by a Doc method and written by a Set method. A
//     read-only field is a single Doc method (pubdate, identifiers, cover).
//
//   - A field says what a value should be, not where it is kept. pkgdoc's
//     slots know where, and this package does not import etree.
//
//   - Each field keeps its EPUB 2 and EPUB 3 branches visible, since the specs
//     differ. EPUB 2 has no standard sort-title mechanism, which a shared
//     writer would hide.
package opf

import (
	"bytes"
	"time"

	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/opf/pkgdoc"
)

// Author mirrors epub.Author.
type Author struct{ Name, SortName string }

// Series mirrors epub.Series.
type Series struct{ Name, Index string }

type Metadata struct {
	Title       string
	SortTitle   string
	Authors     []Author
	Series      *Series // nil when the document records none
	Description string
	Language    string
	Pubdate     string
	Identifiers map[string]string
	CoverPath   string
}

type Doc struct{ d *pkgdoc.Doc }

func Parse(b []byte) (*Doc, error) {
	d, err := pkgdoc.Parse(b)
	if err != nil {
		return nil, err
	}
	return &Doc{d}, nil
}

func (o *Doc) Bytes() ([]byte, error) { return o.d.Bytes() }

// Apply runs edit and reports whether the document changed. A false means the
// file already said what the edit asked for.
//
// It takes a function rather than a struct of fields, so the dcterms:modified
// update below brackets every write however the caller drives the setters.
func (o *Doc) Apply(edit func(*Doc)) bool {
	before, _ := o.Bytes()

	edit(o)

	// §5.5.5 asks for the timestamp when the creator makes changes, and an
	// edit asking for what the file already says is not one. Only the bytes
	// can tell, since a set may find the value already there.
	if after, _ := o.Bytes(); bytes.Equal(before, after) {
		return false
	}
	o.modified().set(time.Now())
	return true
}

// SetTitle writes the title and its sort title; a nil half is left alone.
//
// Writing a title drops the other dc:title segments, and any sort title not
// passed with it. So a caller always passes the sort title, and passes a nil
// title to change the sort title alone.
func (o *Doc) SetTitle(title, sort *string) { o.title().set(title, sort) }

func (o *Doc) SetDescription(v string) { o.d.DC("description").Set(v) }

func (o *Doc) SetLanguage(v string) { o.d.DC("language").Set(v) }

func (o *Doc) SetAuthors(authors []Author) { o.authors().set(authors) }

// SetSeries writes the series, or clears it when s is nil. Both halves are
// written, so a caller changing one passes the other as read.
func (o *Doc) SetSeries(s *Series) { o.series().set(s) }

// Metadata reads the book's metadata. base is the package document's
// directory, used to resolve the cover href.
//
// Nothing is rejected or defaulted. A book with no title, or a series position
// the spec disallows, is reported as written, and a caller with a policy
// applies it.
func (o *Doc) Metadata(base string) Metadata {
	// §5.5.2 allows only whitespace collapsing, which get already did.
	title, sortTitle := o.title().get()
	// TODO: decide whether to derive a sort title when none is set. calibre
	// strips leading articles ("The Hobbit" -> "Hobbit, The"), but that depends
	// on the language.
	return Metadata{
		Title:       title,
		SortTitle:   sortTitle,
		Authors:     o.authors().get(),
		Series:      o.series().get(),
		Description: o.description(),
		Language:    o.language(),
		Pubdate:     o.pubdate(),
		Identifiers: o.identifiers(),
		CoverPath:   o.cover(base),
	}
}

// writeV3 reports whether the EPUB 3 slot takes the value, for a field recorded
// both the EPUB 3 way and as a calibre meta (sort title and series).
//
// A slot already in the file is rewritten whatever the version says, since a
// stale one would outrank the calibre meta on read. A v3 package without one
// gets one; a v2 package does not.
func writeV3(d *pkgdoc.Doc, present bool) bool { return present || d.EPUB3() }

// writeCalibre reports whether the calibre meta takes the value. A v2 package
// always gets one, having no standard mechanism. A v3 package keeps one in step
// only if it already had one.
func writeCalibre(d *pkgdoc.Doc, present bool) bool { return !d.EPUB3() || present }
