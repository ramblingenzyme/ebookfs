package pkgdoc

import (
	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/xml"
)

// How opf obtains a slot. Every entry point the parent package calls to reach
// a value is here; slot.go is what it can then do with one.
//
// A constructor takes the element when the caller already found it and nil when
// the value is not in the document yet, so a read and the write that follows
// address the same place.

// dcSlot is the shared constructor. The id stem is derived from the tag rather
// than passed, so the callers cannot disagree about it.
func (d *Doc) dcSlot(tag string, el *etree.Element) *Element {
	return &Element{
		d:        d,
		el:       el,
		idPrefix: "ebookfs-" + tag,
		newEl:    func() *etree.Element { return etree.NewElement(xml.Qualify(d.dcPrefix(), tag)) },
		parent:   d.md.dcParent,
	}
}

// DC is the Dublin Core element a read and a write of a field both mean,
// created in the right parent if the file has none.
func (d *Doc) DC(tag string) *Element { return d.dcSlot(tag, d.md.primary(tag)) }

// DCAll is every one of them, for the fields that are a list (creators) or that
// have to reconcile the extras (titles).
func (d *Doc) DCAll(tag string) []*Element {
	els := d.md.children(tag)
	out := make([]*Element, len(els))
	for i, el := range els {
		out[i] = d.dcSlot(tag, el)
	}
	return out
}

// NewDC is one the file does not carry yet, whatever else it holds: a creator
// added to the end of the author list, not the creator a read would return.
func (d *Doc) NewDC(tag string) *Element { return d.dcSlot(tag, nil) }

// metaSlot is the <meta property="..."> constructor. The id stem is passed, not
// derived: a property name makes no sensible one.
func (d *Doc) metaSlot(property, idPrefix string, el *etree.Element) *Element {
	return &Element{
		d:        d,
		el:       el,
		idPrefix: idPrefix,
		newEl: func() *etree.Element {
			m := etree.NewElement("meta")
			// spell, not property: in a document that rebound the vocabulary
			// this name resolves in, the literal would mean something else to
			// every other reader.
			m.CreateAttr("property", d.vocab.spell(property))
			return m
		},
		parent: d.md.metaParent,
	}
}

// UnrefinedMeta is a <meta property="..."> carrying a value for the package
// itself rather than for another element in it.
//
// It takes no id stem, unlike the other metaSlot constructors: an id is minted
// only to bind a refinement, and nothing refines a meta that refines nothing.
func (d *Doc) UnrefinedMeta(property string) *Element {
	for _, m := range d.md.children("meta") {
		if d.vocab.Same(attr(m, "property"), property) && attr(m, "refines") == "" {
			return d.metaSlot(property, "", m)
		}
	}
	return d.metaSlot(property, "", nil)
}

// PropertyMetas is every meta carrying the property, refining or not, for a
// field that picks among them by some rule of its own.
func (d *Doc) PropertyMetas(property, idPrefix string) []*Element {
	var out []*Element
	for _, m := range d.md.children("meta") {
		if d.vocab.Same(attr(m, "property"), property) {
			out = append(out, d.metaSlot(property, idPrefix, m))
		}
	}
	return out
}

// NewPropertyMeta is one the file does not carry yet.
func (d *Doc) NewPropertyMeta(property, idPrefix string) *Element {
	return d.metaSlot(property, idPrefix, nil)
}

func (d *Doc) Named(name string) *Named { return &Named{d: d, name: name} }
