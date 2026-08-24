// Package content edits EPUB content documents (§6): the XHTML that makes up
// the book itself, as opposed to the package document describing it. Only one
// thing in them is ours to touch — the dimensions a cover page hardcodes for
// the cover image — and that is repair rather than authoring: an edit here is
// undoing damage another edit would otherwise have done.
//
// The zip container belongs to the parent epub package, and which document is
// the cover page is the package document's answer to give (opf.CoverPages).
//
// Slots are the package document's split, kept for the same reason: a field
// says what a value should be, a slot knows where the document records it. The
// one slot here is an attribute that already exists — see attrSlot for why that
// distinction is the whole safety rule of this package.
package content

import (
	"bytes"
	stdxml "encoding/xml"
	"errors"
	"fmt"
	"path"
	"strconv"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub/xml"
)

type Doc struct {
	doc  *etree.Document
	base string // the document's own directory, for resolving its references
}

// Parse reads a content document. entry is its path in the container, which
// fixes what the references inside it resolve against.
//
// HTML's named entities are declared to the parser: an XHTML content document
// may use any of them (§6.1.2 makes these HTML documents in XML syntax), and
// encoding/xml knows only the five XML ones. Without this a stray &nbsp; would
// read as a syntax error.
//
// Validated as it reads, for the reason ncx.Parse gives: this package only ever
// parses a document in order to write it back, so one that cannot be
// round-tripped is one to leave alone — and the caller skips what this rejects.
func Parse(b []byte, entry string) (*Doc, error) {
	doc := etree.NewDocument()
	doc.ReadSettings.Entity = stdxml.HTMLEntity
	doc.ReadSettings.ValidateInput = true
	// Content documents are where CDATA earns its keep: a <style> or <script>
	// block uses one so it can contain < and & unescaped. Re-encoding that as
	// escaped text leaves the document meaning the same thing to an XML parser
	// and something else to the lenient HTML parsers reading systems use.
	doc.ReadSettings.PreserveCData = true
	if err := doc.ReadFromBytes(b); err != nil {
		return nil, err
	}
	if doc.Root() == nil {
		return nil, errors.New("no root element")
	}
	return &Doc{doc: doc, base: path.Dir(entry)}, nil
}

func (d *Doc) Bytes() ([]byte, error) { return d.doc.WriteToBytes() }

// FitCover resizes the cover image's frame to width by height, and reports
// whether the document said anything about it to change. A document that does
// not reference the cover image is not a cover page, and is left alone.
//
// The frame matters because the usual cover page is an SVG wrapper whose
// viewBox is the old image's pixel dimensions. That viewBox is the coordinate
// space the image is drawn into, so a replacement of a different size is
// cropped or letterboxed by it — the one part of the book that a cover
// replacement can visibly break while every file in it stays valid.
func (d *Doc) FitCover(coverPath string, width, height int) bool {
	before, _ := d.Bytes()

	for _, img := range d.coverImages(coverPath) {
		slot(img, "width").set(strconv.Itoa(width))
		slot(img, "height").set(strconv.Itoa(height))

		// The viewBox belongs to the svg the image is drawn into, which is the
		// nearest ancestor rather than the document's only svg: a cover page
		// may nest one, and the outer one's coordinate space is not the one
		// this image sits in.
		if svg := ancestor(img, "svg"); svg != nil {
			slot(svg, "viewBox").set(fmt.Sprintf("0 0 %d %d", width, height))
		}
	}

	after, _ := d.Bytes()
	return !bytes.Equal(before, after)
}

// coverImages returns the elements drawing the cover image: SVG <image> and
// HTML <img>, whose reference resolves to the same container path the package
// document gave for the cover.
//
// The attribute is matched by local name, so xlink:href and a bare href are
// both found — §6.2.3 leaves an EPUB free to use either, and content documents
// in the wild use both.
func (d *Doc) coverImages(coverPath string) []*etree.Element {
	var out []*etree.Element
	for _, tag := range []string{"image", "img"} {
		for _, el := range d.doc.FindElements("//" + tag) {
			ref := el.SelectAttrValue("href", el.SelectAttrValue("src", ""))
			if ref != "" && xml.ResolveHref(d.base, ref) == coverPath {
				out = append(out, el)
			}
		}
	}
	return out
}

// ancestor walks up from el to the nearest enclosing element with the given
// tag, or nil.
func ancestor(el *etree.Element, tag string) *etree.Element {
	for p := el.Parent(); p != nil; p = p.Parent() {
		if p.Tag == tag {
			return p
		}
	}
	return nil
}

// A slot is one string value together with the place in the document that
// records it, so nothing above has to touch etree. The one place here is an
// attribute.
//
// Write-only, and never a create: an attribute the document does not already
// carry is one it does not depend on, and adding it would be authoring layout
// rather than repairing it. A cover page sizing its image in CSS, or leaving
// the svg to fill the viewport, is already correct at any size and must come
// out of an edit unchanged.
type attrSlot struct {
	el   *etree.Element
	name string
}

func slot(el *etree.Element, name string) attrSlot { return attrSlot{el: el, name: name} }

// set is a no-op unless the attribute is already there, whatever prefix it
// carries — etree matches an attribute by local name, so a document using an
// explicit svg: prefix keeps its own spelling rather than gaining a second
// attribute that shadows it.
func (s attrSlot) set(value string) {
	if a := s.el.SelectAttr(s.name); a != nil {
		a.Value = value
	}
}
