// Package opf reads and writes the EPUB package document: the .opf file holding
// a book's metadata. The zip container around it belongs to the parent epub
// package, and the XML under it to pkgdoc.
//
// A field is one piece of metadata this package models. Reading (get) and
// writing (set) both go through it so the two cannot disagree, and set(get())
// never invents metadata the file did not carry. Nothing here defaults a value
// or rejects a document for what it says; Doc.Metadata says what that costs the
// caller.
//
// Three rules shape the fields:
//
//   - A field with a write side is a type with get/set (title, authors, series,
//     modified). A read-only field is a single Doc method (description,
//     language, pubdate, identifiers, cover).
//
//   - A field says what a value should be, never where it is kept. The slots
//     pkgdoc hands out know where, and this package does not import etree.
//
//   - The EPUB 2 / EPUB 3 branch stays visible in each field. The specs differ,
//     and v2 has no sort-title mechanism at all, which a common writer would
//     hide.
package opf

import (
	"bytes"
	"time"

	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/opf/pkgdoc"
)

// Author is a creator this package owns: one carrying the "aut" MARC relator,
// or carrying no role at all.
type Author struct{ Name, SortName string }

// Series is a book's membership of a collection. Index is a string because
// D.3.7 allows multi-level positions such as 2.2.1, which no number holds.
type Series struct{ Name, Index string }

// Metadata is the package document as this package reads it, every value
// verbatim. A book with no title reads back an empty Title.
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

// Where a field is recorded twice, once the way its spec version says and once
// as the proprietary meta calibre writes, these decide which halves a write
// touches. Title sort and series are both encoded that way and both ask here,
// so the rule is stated once even though each field keeps its own branch.

// writeV3 reports whether the EPUB 3 slot takes the value. One already in the
// file is rewritten whatever version the package claims, since leaving it stale
// would outrank the calibre meta on the way back in. A v3 package without one
// gets one; a v2 package without one stays without.
func writeV3(d *pkgdoc.Doc, present bool) bool { return present || d.EPUB3() }

// writeCalibre reports whether the calibre meta takes the value. A v2 package
// always gets one, having no standard mechanism; a v3 package only if it
// already carried one, kept in step rather than left contradicting the v3 slot.
func writeCalibre(d *pkgdoc.Doc, present bool) bool { return !d.EPUB3() || present }

func Parse(b []byte) (*Doc, error) {
	d, err := pkgdoc.Parse(b)
	if err != nil {
		return nil, err
	}
	return &Doc{d}, nil
}

func (o *Doc) Bytes() ([]byte, error) { return o.d.Bytes() }

// Apply runs edit against the document and reports whether that changed
// anything; nothing is serialized until Bytes. A false means the file already
// said what the edit asked for, so the caller has nothing to write back.
//
// It takes the edit as a function rather than a struct of fields so that the
// two rules below hold however the caller drives the setters: they must bracket
// every write, and nothing outside this package can write without them.
func (o *Doc) Apply(edit func(*Doc)) bool {
	before, _ := o.Bytes()

	edit(o)

	// §5.5.5 asks for the timestamp when the creator makes changes, so an edit
	// that asks for what the file already says is not one. Comparing the whole
	// serialization is the only honest test of that: a field's set is free to
	// decide the document already carries the value, and only the bytes know.
	if after, _ := o.Bytes(); bytes.Equal(before, after) {
		return false
	}
	o.modified().set(time.Now())
	return true
}

// SetTitle writes the title and its sort value. A nil half is one the caller
// did not touch.
//
// The halves are coupled. Writing a title takes the document's other dc:title
// segments with it, and a title written without a sort value drops the one the
// book carried. So a caller edits the sort value alone by passing a nil title,
// and restates the sort value whenever it writes a title.
func (o *Doc) SetTitle(title, sort *string) { o.title().set(title, sort) }

func (o *Doc) SetDescription(v string) { o.d.DC("description").Set(v) }

func (o *Doc) SetLanguage(v string) { o.d.DC("language").Set(v) }

func (o *Doc) SetAuthors(authors []Author) { o.authors().set(authors) }

// SetSeries writes the series membership, or clears it when s is nil. Both
// halves are stated, so a caller changing one reads the other back first.
func (o *Doc) SetSeries(s *Series) { o.series().set(s) }

// Metadata reads the book's metadata out of the document. base is the OPF's own
// directory, needed only to resolve the cover href.
//
// Nothing is rejected and nothing is defaulted. A book with no title or no
// authors is a fact about the file, and a series position the spec disallows is
// reported as written. Requiring a title, or showing something in place of a bad
// position, is a policy, so it lives with the caller that has one.
func (o *Doc) Metadata(base string) Metadata {
	// Reported as written. §5.5.2 licenses stripping and collapsing whitespace,
	// which get already did, and nothing else: a value is text, not a path
	// component. Making it safe to use as one is the business of whoever builds
	// the path.
	title, sortTitle := o.title().get()
	// TODO: decide whether to derive a sort title heuristically when none is set
	// (calibre strips leading articles, e.g. "The Hobbit" -> "Hobbit, The"); it is
	// language-dependent, so for now an unset sort title is left empty.
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
