// Package content edits EPUB content documents (§6), the XHTML that makes up
// the book. Its one job is resizing a cover page to fit a replaced cover image.
// opf.CoverPages finds the cover page.
package content

import (
	"bytes"
	stdxml "encoding/xml"
	"errors"
	"fmt"
	"path"
	"strconv"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/xml"
)

type Doc struct {
	doc  *etree.Document
	base string // the document's directory
}

// Parse reads a content document. entry is its path in the container, which
// its references resolve against.
func Parse(b []byte, entry string) (*Doc, error) {
	doc := etree.NewDocument()
	// §6.1.2 makes these HTML documents in XML syntax, so any HTML named entity
	// may appear; encoding/xml knows only the five XML ones.
	doc.ReadSettings.Entity = stdxml.HTMLEntity
	// pkgdoc.Parse says why this document validates and that one does not.
	doc.ReadSettings.ValidateInput = true
	// A <style> or <script> uses CDATA to hold < and & unescaped. Re-encoded,
	// it means something else to the HTML parsers reading systems use.
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
// whether anything changed. A document that does not reference the cover image
// is left alone.
//
// The usual cover page is an SVG whose viewBox is the old image's pixel size,
// so a replacement of different proportions is drawn cropped.
func (d *Doc) FitCover(coverPath string, width, height int) bool {
	before, _ := d.Bytes()

	for _, img := range d.coverImages(coverPath) {
		slot(img, "width").set(strconv.Itoa(width))
		slot(img, "height").set(strconv.Itoa(height))

		// The nearest ancestor, since a nested svg has its own coordinate
		// space.
		if svg := ancestor(img, "svg"); svg != nil {
			slot(svg, "viewBox").set(fmt.Sprintf("0 0 %d %d", width, height))
		}
	}

	after, _ := d.Bytes()
	return !bytes.Equal(before, after)
}

// coverImages returns the SVG <image> and HTML <img> elements that reference
// the cover. Attributes match by local name, so xlink:href and a bare href are
// both found; §6.2.3 allows either.
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

func ancestor(el *etree.Element, tag string) *etree.Element {
	for p := el.Parent(); p != nil; p = p.Parent() {
		if p.Tag == tag {
			return p
		}
	}
	return nil
}

// attrSlot never creates an attribute. A page sizing its image in CSS already
// fits any size, so adding one would change its layout.
type attrSlot struct {
	el   *etree.Element
	name string
}

func slot(el *etree.Element, name string) attrSlot { return attrSlot{el: el, name: name} }

// set matches the attribute whatever its prefix, so a document keeps its own
// spelling rather than gaining a second attribute.
func (s attrSlot) set(value string) {
	if a := s.el.SelectAttr(s.name); a != nil {
		a.Value = value
	}
}
