package pkgdoc

import "github.com/beevik/etree"

// A Slot is one string value together with the place in the document that
// records it. The opf package decides what a value should be; a slot knows how
// the package document stores it, so nothing above here touches etree.
//
// Set and Clear stay separate rather than overloading Set(""): an empty sort
// title means "remove the refinement", but an empty description means "blank
// the element", and both behaviours have to survive.
type Slot interface {
	Set(value string)
	Clear()
}

// Put is set-or-clear, for the places where an empty value means absent.
func Put(s Slot, value string) {
	if value == "" {
		s.Clear()
		return
	}
	s.Set(value)
}

// Element is an element's text, and the anchor for the refinements and
// opf: attributes that hang off it. The element may not exist yet: ensure
// materialises it on the first write, so a field can describe a value without
// knowing whether the file already carries one.
//
// newEl and parent are separate because a field may own the order its elements
// sit in — see Place.
type Element struct {
	d        *Doc
	el       *etree.Element        // nil until found or created
	newEl    func() *etree.Element // detached
	parent   func() *etree.Element // where it belongs
	idPrefix string
}

func (e *Element) Exists() bool { return e.el != nil }

func (e *Element) Get() string { return text(e.el) }

// Same reports whether two slots stand for one element. Slots are minted per
// call, so a field holding on to one has to ask.
func (e *Element) Same(other *Element) bool {
	return e.el != nil && other != nil && e.el == other.el
}

func (e *Element) ensure() *etree.Element {
	if e.el == nil {
		e.el = e.newEl()
		e.parent().AddChild(e.el)
	}
	return e.el
}

// Place moves the element to the end of its parent, for the fields that own the
// order theirs sit in. AddChild moves an element already in the tree rather than
// duplicating it.
func (e *Element) Place() { e.parent().AddChild(e.ensure()) }

// Remove takes the element and the refinements bound to it, and unbinds the
// slot, so a later write creates a fresh element rather than resurrecting a
// detached one.
//
// ponytail: rescans the metadata per element removed, where the callers used to
// batch one scan. Tens of elements; batch again only if a profile says to.
func (e *Element) Remove() {
	if e.el == nil {
		return
	}
	e.d.removeRefinements([]string{e.ID()})
	detach(e.el)
	e.el = nil
}

func (e *Element) Set(value string) { e.ensure().SetText(value) }

// ID reads the element's id without minting one: a read must not modify the
// document, and an element with no id can carry no refinements anyway.
func (e *Element) ID() string {
	if e.el == nil {
		return ""
	}
	return attr(e.el, "id")
}

// mintID mints one on demand, since a refinement can only bind to an element
// that has an id to bind to.
func (e *Element) mintID() string { return e.d.ensureID(e.ensure(), e.idPrefix) }

func (e *Element) Refine(property string) *Refine {
	return &Refine{d: e.d, owner: e, property: property}
}

func (e *Element) OPFAttr(name string) *OPFAttr {
	return &OPFAttr{d: e.d, owner: e, name: name}
}

// Refine is an EPUB 3 refinement: a <meta property="..."> bound to the
// owner element by id.
type Refine struct {
	d             *Doc
	owner         *Element
	property      string
	unschemedOnly bool
	schemedAs     string
}

// Unschemed narrows to the refinements carrying no scheme, for a property whose
// meaning depends on one: D.3.4 defines series and set only "when no scheme is
// specified", so a value from someone else's code list is neither.
func (r *Refine) Unschemed() *Refine {
	narrowed := *r
	narrowed.unschemedOnly = true
	narrowed.schemedAs = ""
	return &narrowed
}

// Schemed is the other half of the same narrowing: the refinements whose value
// is a code in the named list, such as an identifier-type from onix:codelist5.
// The scheme is matched through the vocabulary, so a document that rebound the
// prefix is read on its own terms.
func (r *Refine) Schemed(scheme string) *Refine {
	narrowed := *r
	narrowed.unschemedOnly = false
	narrowed.schemedAs = scheme
	return &narrowed
}

func (r *Refine) elements() []*etree.Element {
	ms := r.d.refineElements(r.owner.ID(), r.property)
	if !r.unschemedOnly && r.schemedAs == "" {
		return ms
	}
	var out []*etree.Element
	for _, m := range ms {
		scheme := attr(m, "scheme")
		if r.unschemedOnly && scheme != "" {
			continue
		}
		if r.schemedAs != "" && !r.d.vocab.Same(scheme, r.schemedAs) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func (r *Refine) Get() string {
	if ms := r.elements(); len(ms) > 0 {
		return text(ms[0])
	}
	return ""
}

// Set updates the refinement already there rather than replacing it, so it
// keeps its position in the document.
//
// ponytail: duplicates of one property are left in place and only the first is
// updated. Revisit if epubcheck rejects a file we wrote.
func (r *Refine) Set(value string) {
	if ms := r.elements(); len(ms) > 0 {
		ms[0].SetText(value)
		return
	}
	r.Add(value, "")
}

func (r *Refine) Exists() bool { return len(r.elements()) > 0 }

func (r *Refine) Clear() {
	for _, m := range r.elements() {
		detach(m)
	}
}

// Values returns every refinement of this property, for the ones the vocabulary
// allows to repeat: role is "zero or more" (D.3.10).
func (r *Refine) Values() []string {
	var out []string
	for _, m := range r.elements() {
		if v := text(m); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// Add appends unconditionally, for properties where an existing value may be
// one we do not own, such as a creator's second role.
func (r *Refine) Add(value, scheme string) {
	r.d.addRefine(r.owner.mintID(), r.property, value, scheme)
}

// OPFAttr is an EPUB 2 opf: attribute on an element, such as opf:role or
// opf:file-as. The namespace prefix is this slot's business, not a field's.
type OPFAttr struct {
	d     *Doc
	owner *Element
	name  string
}

func (a *OPFAttr) Get() string {
	if a.owner.el == nil {
		return ""
	}
	return attr(a.owner.el, a.name)
}

// Set writes the attribute Get would read. etree matches an attribute by local
// name whatever prefix it carries, so a document with a bare file-as rather than
// opf:file-as is read from that one; creating the qualified spelling regardless
// would leave the element asserting two sort names, with the next read taking
// the stale one. A creator with no such attribute gets the qualified spelling.
func (a *OPFAttr) Set(value string) {
	el := a.owner.ensure()
	if existing := el.SelectAttr(a.name); existing != nil {
		existing.Value = value
		return
	}
	el.CreateAttr(qualify(a.d.ns.opf(), a.name), value)
}

func (a *OPFAttr) Clear() {
	if a.owner.el == nil {
		return
	}
	if existing := a.owner.el.SelectAttr(a.name); existing != nil {
		a.owner.el.RemoveAttr(existing.FullKey())
	}
}

// Named is the EPUB 2 <meta name="..." content="..."> pair that
// predates refinements.
type Named struct {
	d    *Doc
	name string
}

func (n *Named) Get() string {
	if ms := n.d.md.named(n.name); len(ms) > 0 {
		return attr(ms[0], "content")
	}
	return ""
}

func (n *Named) Exists() bool { return len(n.d.md.named(n.name)) > 0 }

// Set updates the meta already there rather than replacing it, so it keeps its
// position in the document.
func (n *Named) Set(value string) {
	if ms := n.d.md.named(n.name); len(ms) > 0 {
		ms[0].CreateAttr("content", value)
		return
	}
	m := n.d.md.metaParent().CreateElement("meta")
	m.CreateAttr("name", n.name)
	m.CreateAttr("content", value)
}

func (n *Named) Clear() {
	for _, m := range n.d.md.named(n.name) {
		detach(m)
	}
}

// dcSlot is the shared constructor. The id stem is derived from the tag rather
// than passed, so the callers cannot disagree about it.
func (d *Doc) dcSlot(tag string, el *etree.Element) *Element {
	return &Element{
		d:        d,
		el:       el,
		idPrefix: "ebookfs-" + tag,
		newEl:    func() *etree.Element { return etree.NewElement(qualify(d.dcPrefix(), tag)) },
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
			// our name resolves in, the literal would mean something else to
			// every other reader.
			m.CreateAttr("property", d.vocab.spell(property))
			return m
		},
		parent: d.md.metaParent,
	}
}

// UnrefinedMeta is a <meta property="..."> carrying a value for the package
// itself rather than for another element in it.
func (d *Doc) UnrefinedMeta(property, idPrefix string) *Element {
	for _, m := range d.md.children("meta") {
		if d.vocab.Same(attr(m, "property"), property) && attr(m, "refines") == "" {
			return d.metaSlot(property, idPrefix, m)
		}
	}
	return d.metaSlot(property, idPrefix, nil)
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
