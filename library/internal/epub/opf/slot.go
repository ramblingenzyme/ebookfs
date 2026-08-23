package opf

import "github.com/beevik/etree"

// A slot is one string value together with the place in the document that
// records it. Fields decide what a value should be; slots know how the package
// document stores it, so no field_*.go has to touch etree.
//
// set and clear stay separate rather than overloading set(""): an empty sort
// title means "remove the refinement", but an empty description means "blank
// the element", and both behaviours have to survive.
type slot interface {
	set(value string)
	clear()
}

// put is set-or-clear, for the places where an empty value means absent.
func put(s slot, value string) {
	if value == "" {
		s.clear()
		return
	}
	s.set(value)
}

// elementSlot is an element's text, and the anchor for the refinements and
// opf: attributes that hang off it. The element may not exist yet: create
// materialises it on the first write, so a field can describe a value without
// knowing whether the file already carries one.
type elementSlot struct {
	o        *Doc
	el       *etree.Element // nil until found or created
	create   func() *etree.Element
	idPrefix string
}

func (e *elementSlot) exists() bool { return e.el != nil }

func (e *elementSlot) get() string { return text(e.el) }

// ensure materialises the element if the file carries none, so a field can
// describe a value without knowing whether one is already there.
func (e *elementSlot) ensure() *etree.Element {
	if e.el == nil {
		e.el = e.create()
	}
	return e.el
}

func (e *elementSlot) set(value string) { e.ensure().SetText(value) }

// id mints one on demand, since a refinement can only bind to an element that
// has an id to bind to.
func (e *elementSlot) id() string { return e.o.ensureID(e.ensure(), e.idPrefix) }

func (e *elementSlot) refine(property string) *refineSlot {
	return &refineSlot{o: e.o, owner: e, property: property}
}

func (e *elementSlot) opfAttr(name string) *opfAttrSlot {
	return &opfAttrSlot{o: e.o, owner: e, name: name}
}

// refineSlot is an EPUB 3 refinement: a <meta property="..."> bound to the
// owner element by id.
type refineSlot struct {
	o        *Doc
	owner    *elementSlot
	property string
}

// ownerID reads the owner's id without minting one — a read must not modify the
// document, and an owner with no id can carry no refinements anyway.
func (r *refineSlot) ownerID() string {
	if r.owner.el == nil {
		return ""
	}
	return attr(r.owner.el, "id")
}

func (r *refineSlot) get() string {
	if ms := r.o.refineElements(r.ownerID(), r.property); len(ms) > 0 {
		return text(ms[0])
	}
	return ""
}

// set updates the refinement already there rather than replacing it, so it
// keeps its position in the document.
//
// ponytail: duplicates of one property are left in place and only the first is
// updated. Revisit if epubcheck rejects a file we wrote.
func (r *refineSlot) set(value string) {
	if ms := r.o.refineElements(r.ownerID(), r.property); len(ms) > 0 {
		ms[0].SetText(value)
		return
	}
	r.add(value, "")
}

func (r *refineSlot) exists() bool {
	return len(r.o.refineElements(r.ownerID(), r.property)) > 0
}

func (r *refineSlot) clear() {
	for _, m := range r.o.refineElements(r.ownerID(), r.property) {
		detach(m)
	}
}

// values returns every refinement of this property, for the ones the vocabulary
// allows to repeat: role is "zero or more" (D.3.10).
func (r *refineSlot) values() []string {
	var out []string
	for _, m := range r.o.refineElements(r.ownerID(), r.property) {
		if v := text(m); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// add appends unconditionally, for properties where an existing value may be
// one we do not own, such as a creator's second role.
func (r *refineSlot) add(value, scheme string) {
	r.o.addRefine(r.owner.id(), r.property, value, scheme)
}

// opfAttrSlot is an EPUB 2 opf: attribute on an element, such as opf:role or
// opf:file-as. The namespace prefix is this slot's business, not a field's.
type opfAttrSlot struct {
	o     *Doc
	owner *elementSlot
	name  string
}

func (a *opfAttrSlot) get() string {
	if a.owner.el == nil {
		return ""
	}
	return attr(a.owner.el, a.name)
}

// set writes the attribute get would read. etree matches an attribute by local
// name whatever prefix it carries, so a document with a bare file-as rather than
// opf:file-as is read from that one; creating the qualified spelling regardless
// would leave the element asserting two sort names, with the next read taking
// the stale one. A creator with no such attribute gets the qualified spelling.
func (a *opfAttrSlot) set(value string) {
	el := a.owner.ensure()
	if existing := el.SelectAttr(a.name); existing != nil {
		existing.Value = value
		return
	}
	el.CreateAttr(qualify(a.o.ensureOPFPrefix(), a.name), value)
}

func (a *opfAttrSlot) clear() {
	if a.owner.el == nil {
		return
	}
	if existing := a.owner.el.SelectAttr(a.name); existing != nil {
		a.owner.el.RemoveAttr(existing.FullKey())
	}
}

// namedMetaSlot is the EPUB 2 <meta name="..." content="..."> pair that
// predates refinements.
type namedMetaSlot struct {
	o    *Doc
	name string
}

func (n *namedMetaSlot) get() string {
	if ms := n.o.namedMetaElements(n.name); len(ms) > 0 {
		return attr(ms[0], "content")
	}
	return ""
}

func (n *namedMetaSlot) exists() bool { return len(n.o.namedMetaElements(n.name)) > 0 }

// set updates the meta already there rather than replacing it, so it keeps its
// position in the document.
func (n *namedMetaSlot) set(value string) {
	if ms := n.o.namedMetaElements(n.name); len(ms) > 0 {
		ms[0].CreateAttr("content", value)
		return
	}
	m := n.o.metaParent().CreateElement("meta")
	m.CreateAttr("name", n.name)
	m.CreateAttr("content", value)
}

func (n *namedMetaSlot) clear() {
	for _, m := range n.o.namedMetaElements(n.name) {
		detach(m)
	}
}

// dcElement is the Dublin Core element a read and a write of a field both mean,
// created in the right parent if the file has none.
func (o *Doc) dcElement(tag, idPrefix string) *elementSlot {
	return &elementSlot{
		o:        o,
		el:       o.primary(tag),
		idPrefix: idPrefix,
		create:   func() *etree.Element { return o.dcParent().CreateElement(qualify(o.dcPrefix(), tag)) },
	}
}

// propertyMeta is an unrefined <meta property="...">: one that carries a value
// for the package itself rather than for another element in it.
func (o *Doc) propertyMeta(property string) *elementSlot {
	var found *etree.Element
	for _, m := range o.elements("meta") {
		if o.sameProperty(attr(m, "property"), property) && attr(m, "refines") == "" {
			found = m
			break
		}
	}
	return &elementSlot{
		o:  o,
		el: found,
		create: func() *etree.Element {
			m := o.metaParent().CreateElement("meta")
			// spell, not property: in a document that rebound the vocabulary
			// our name resolves in, the literal would mean something else to
			// every other reader.
			m.CreateAttr("property", o.spell(property))
			return m
		},
	}
}

func (o *Doc) namedMeta(name string) *namedMetaSlot { return &namedMetaSlot{o: o, name: name} }
