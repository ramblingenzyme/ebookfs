// Package content edits EPUB content documents (§6): the XHTML making up the
// book itself. One thing in them is ours — the dimensions a cover page
// hardcodes for the cover image — and that is repair, undoing damage another
// edit would otherwise do. opf.CoverPages says which document is the cover page.
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

// Parse reads a content document. entry is its container path, which fixes what
// the references inside it resolve against. Validated as ncx.Parse is.
func Parse(b []byte, entry string) (*Doc, error) {
	doc := etree.NewDocument()
	// §6.1.2 makes these HTML documents in XML syntax, so any HTML named entity
	// may appear; encoding/xml knows only the five XML ones.
	doc.ReadSettings.Entity = stdxml.HTMLEntity
	doc.ReadSettings.ValidateInput = true
	// A <style> or <script> uses CDATA to hold < and & unescaped. Re-encoded, it
	// means the same to an XML parser and something else to the lenient HTML
	// parsers reading systems use.
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

// FitCover resizes the cover image's frame to width by height and reports
// whether the document said anything about it to change; one not referencing
// the cover image is not a cover page and is left alone.
//
// The usual cover page is an SVG wrapper whose viewBox — the coordinate space
// the image is drawn into — is the old image's pixel size, so a replacement of
// different proportions renders cropped while every file stays valid.
func (d *Doc) FitCover(coverPath string, width, height int) bool {
	before, _ := d.Bytes()

	for _, img := range d.coverImages(coverPath) {
		slot(img, "width").set(strconv.Itoa(width))
		slot(img, "height").set(strconv.Itoa(height))

		// Nearest ancestor, not the document's only svg: a nested one's
		// coordinate space is not the one this image sits in.
		if svg := ancestor(img, "svg"); svg != nil {
			slot(svg, "viewBox").set(fmt.Sprintf("0 0 %d %d", width, height))
		}
	}

	after, _ := d.Bytes()
	return !bytes.Equal(before, after)
}

// coverImages returns the SVG <image> and HTML <img> elements whose reference
// resolves to the cover's container path. Matched by local name, so xlink:href
// and a bare href are both found — §6.2.3 allows either.
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

// ancestor is the nearest enclosing element with the given tag, or nil.
func ancestor(el *etree.Element, tag string) *etree.Element {
	for p := el.Parent(); p != nil; p = p.Parent() {
		if p.Tag == tag {
			return p
		}
	}
	return nil
}

// An attribute, and never a create: a page sizing its image in CSS is already
// correct at any size, so adding one would be authoring layout, not repairing
// it.
type attrSlot struct {
	el   *etree.Element
	name string
}

func slot(el *etree.Element, name string) attrSlot { return attrSlot{el: el, name: name} }

// set is a no-op unless the attribute is already there, whatever prefix it
// carries, so a document using an explicit one keeps its own spelling rather
// than gaining a second attribute that shadows it.
func (s attrSlot) set(value string) {
	if a := s.el.SelectAttr(s.name); a != nil {
		a.Value = value
	}
}
