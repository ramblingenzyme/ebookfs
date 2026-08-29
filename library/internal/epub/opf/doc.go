// Package opf reads and writes the EPUB package document: the .opf file holding
// a book's metadata. The zip container around it belongs to the parent epub
// package, and the XML under it to pkgdoc.
//
// A field is one piece of metadata ebookfs owns. Reading (get) and writing (set)
// both go through it so the two cannot disagree. Book-level validation and
// presentation defaults live in Bib instead, so set(get()) never invents
// metadata the file did not carry.
//
// Three rules keep the fields readable:
//
//   - A field with a write side is a type with get/set (title, authors, series,
//     modified). A read-only field is a single Doc method (description,
//     language, pubdate, identifiers, cover).
//
//   - A field says what a value should be, never where it is kept. The slots
//     pkgdoc hands out know where, and nothing here touches the XML: this
//     package does not import etree, and that is the point of the split.
//
//   - The EPUB 2 / EPUB 3 branch stays visible in each field. The specs
//     genuinely differ, and v2 has no sort-title mechanism at all; hiding that
//     behind a common writer would hide what matters.
package opf

import (
	"bytes"
	"errors"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub/opf/pkgdoc"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

type Doc struct{ d *pkgdoc.Doc }

func Parse(b []byte) (*Doc, error) {
	d, err := pkgdoc.Parse(b)
	if err != nil {
		return nil, err
	}
	return &Doc{d}, nil
}

func (o *Doc) Bytes() ([]byte, error) { return o.d.Bytes() }

// Apply writes the edits into the document and reports whether that changed
// anything; nothing is serialized until Bytes. A false means the file already
// said what the edit asked for, so the caller has nothing to write back.
func (o *Doc) Apply(e model.Edits) bool {
	before, _ := o.Bytes()

	o.title().set(e.Title, e.SortTitle)

	if e.Description != nil {
		o.d.DC("description").Set(*e.Description)
	}
	if e.Language != nil {
		o.d.DC("language").Set(*e.Language)
	}
	if e.Authors != nil {
		o.authors().set(*e.Authors)
	}
	if e.Series != nil || e.SeriesIndex != nil {
		o.series().set(e.Series, e.SeriesIndex)
	}

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

// Bib reads the book's metadata out of the document, adding what is not a
// field's business: whole-book validation and presentation defaults. base is the
// OPF's own directory, needed only to resolve the cover href.
func (o *Doc) Bib(base string) (*book.Bib, error) {
	b := &book.Bib{}

	// Reported as written. §5.5.2 licenses stripping and collapsing whitespace,
	// which get already did, and nothing else: a value is text, not a path
	// component. Making it safe to use as one is the business of whoever builds
	// the path — model.PathSafe, called by the store and the 9P names.
	title, sortTitle := o.title().get()
	if title == "" {
		return nil, errors.New("no title")
	}
	b.Title = title
	b.SortTitle = sortTitle
	// TODO: decide whether to derive a sort title heuristically when none is set
	// (calibre strips leading articles, e.g. "The Hobbit" -> "Hobbit, The"); it is
	// language-dependent, so for now an unset sort title is left empty.

	if b.Authors = o.authors().get(); len(b.Authors) == 0 {
		return nil, errors.New("no authors")
	}

	if s := o.series().get(); s != nil {
		// Defaulted here, not in the field, so a rewrite cannot write it back.
		if !model.ValidSeriesIndex(s.Index) {
			s.Index = "1"
		}
		b.Series = s
	}

	b.Description = o.description()
	b.Language = o.language()
	b.Pubdate = o.pubdate()
	b.Identifiers = o.identifiers()
	b.CoverPath = o.cover(base)

	return b, nil
}
