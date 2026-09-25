package pkgdoc

import (
	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/xml"
)

// dcSlot takes nil for an element the file does not have yet, which the first
// write creates. The id stem comes from the tag, so callers cannot disagree
// about it.
func (d *Doc) dcSlot(tag string, el *etree.Element) *Element {
	return &Element{
		d:        d,
		el:       el,
		idPrefix: "ebookfs-" + tag,
		newEl:    func() *etree.Element { return etree.NewElement(xml.Qualify(d.dcPrefix(), tag)) },
		parent:   d.md.dcParent,
	}
}

// DC is the Dublin Core element a read and a write of a field both mean.
func (d *Doc) DC(tag string) *Element { return d.dcSlot(tag, d.md.primary(tag)) }

func (d *Doc) DCAll(tag string) []*Element {
	els := d.md.children(tag)
	out := make([]*Element, len(els))
	for i, el := range els {
		out[i] = d.dcSlot(tag, el)
	}
	return out
}

func (d *Doc) NewDC(tag string) *Element { return d.dcSlot(tag, nil) }

// metaSlot takes its id stem, since a property name makes no sensible one.
func (d *Doc) metaSlot(property, idPrefix string, el *etree.Element) *Element {
	return &Element{
		d:        d,
		el:       el,
		idPrefix: idPrefix,
		newEl: func() *etree.Element {
			m := etree.NewElement("meta")
			// Spelled, since in a document that rebound the vocabulary the
			// literal would mean something else.
			m.CreateAttr("property", d.vocab.spell(property))
			return m
		},
		parent: d.md.metaParent,
	}
}

// UnrefinedMeta is a <meta property> about the package itself rather than
// another element. It needs no id stem: ids are minted only to bind
// refinements, and nothing refines this one.
func (d *Doc) UnrefinedMeta(property string) *Element {
	for _, m := range d.md.children("meta") {
		if d.vocab.Same(attr(m, "property"), property) && attr(m, "refines") == "" {
			return d.metaSlot(property, "", m)
		}
	}
	return d.metaSlot(property, "", nil)
}

func (d *Doc) PropertyMetas(property, idPrefix string) []*Element {
	var out []*Element
	for _, m := range d.md.children("meta") {
		if d.vocab.Same(attr(m, "property"), property) {
			out = append(out, d.metaSlot(property, idPrefix, m))
		}
	}
	return out
}

func (d *Doc) NewPropertyMeta(property, idPrefix string) *Element {
	return d.metaSlot(property, idPrefix, nil)
}

func (d *Doc) Named(name string) *Named { return &Named{d: d, name: name} }
