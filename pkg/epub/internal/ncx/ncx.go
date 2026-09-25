// Package ncx writes the NCX, EPUB 2's table of contents (OPF 2.0 §2.4.1,
// which defers to DAISY/NISO Z39.86 §8). §5.9.5 keeps it as a legacy EPUB 3
// feature. opf.NCXPath finds it.
//
// Its <docTitle> and <docAuthor> copy the title and authors, and a reading
// system navigating by the NCX shows them. Neither spec requires the copies to
// agree, so keeping them in step is ours. Nothing is read back out of the NCX.
//
// A missing element is not created, since where it would go is for Z39.86's
// content model to say.
package ncx

import (
	"bytes"
	"errors"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/xml"
)

type Doc struct {
	doc *etree.Document
	ncx *etree.Element
}

// Parse rejects a document etree would have to correct, rather than writing
// the correction back. It is strict where pkgdoc.Parse is permissive, since a
// rejected NCX is skipped while a rejected package document loses the book.
func Parse(b []byte) (*Doc, error) {
	doc := etree.NewDocument()
	doc.ReadSettings.ValidateInput = true
	// pkgdoc.Parse says why CDATA is preserved.
	doc.ReadSettings.PreserveCData = true
	if err := doc.ReadFromBytes(b); err != nil {
		return nil, err
	}
	ncx := doc.SelectElement("ncx")
	if ncx == nil {
		return nil, errors.New("no <ncx> element")
	}
	return &Doc{doc: doc, ncx: ncx}, nil
}

func (d *Doc) Bytes() ([]byte, error) { return d.doc.WriteToBytes() }

// Apply writes the title and author names, and reports whether that changed
// anything. A nil title or names slice leaves that field alone. It takes names
// because the NCX has nowhere to record a sort name.
func (d *Doc) Apply(title *string, names []string) bool {
	before, _ := d.Bytes()

	if title != nil {
		d.title().set(*title)
	}
	if names != nil {
		d.authors().set(names)
	}

	// Compared as bytes, since a set may find the value already there.
	after, _ := d.Bytes()
	return !bytes.Equal(before, after)
}

func (d *Doc) title() textSlot { return slot(d.ncx.SelectElement("docTitle")) }

type authorsField struct{ d *Doc }

func (d *Doc) authors() authorsField { return authorsField{d} }

// set makes the NCX carry one <docAuthor> per author, in order, but only if it
// has any, since an NCX naming no author contradicts nothing. New ones go
// after the last <docAuthor>, because the content model fixes the order of
// <ncx>'s children (head, docTitle, docAuthor*, navMap, …).
func (f authorsField) set(names []string) {
	existing := f.d.ncx.SelectElements("docAuthor")
	if len(existing) == 0 {
		return
	}

	last := existing[len(existing)-1]
	for i, name := range names {
		if i < len(existing) {
			slot(existing[i]).set(name)
			continue
		}
		el := etree.NewElement(xml.Qualify(last.Space, "docAuthor"))
		f.d.ncx.InsertChildAt(last.Index()+1, el)
		slot(el).set(name)
		last = el
	}

	for i := len(names); i < len(existing); i++ {
		f.d.ncx.RemoveChild(existing[i])
	}
}

// textSlot is the <text> child that <docTitle> and <docAuthor> hold their value
// in.
type textSlot struct{ owner *etree.Element }

func slot(owner *etree.Element) textSlot { return textSlot{owner: owner} }

// set does nothing when the owner is missing. A missing <text> is created,
// since Z39.86 requires it in both elements.
func (s textSlot) set(value string) {
	if s.owner == nil {
		return
	}
	t := s.owner.SelectElement("text")
	if t == nil {
		t = s.owner.CreateElement(xml.Qualify(s.owner.Space, "text"))
	}
	t.SetText(value)
}
