package pkgdoc

import (
	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/xml"
)

// Slot is one place the document keeps a string value. Set and Clear are
// separate rather than Set(""), since an empty sort title removes a refinement
// but an empty description blanks an element.
type Slot interface {
	Set(value string)
	Clear()
}

func Put(s Slot, value string) {
	if value == "" {
		s.Clear()
		return
	}
	s.Set(value)
}

// Element is an element's text, and the anchor for its refinements and opf:
// attributes. The element may not exist yet; the first write creates it.
//
// newEl and parent are separate because a field may own the order its elements
// sit in; see Place.
type Element struct {
	d        *Doc
	el       *etree.Element        // nil until found or created
	newEl    func() *etree.Element // detached
	parent   func() *etree.Element // where it belongs
	idPrefix string
}

func (e *Element) Exists() bool { return e.el != nil }

func (e *Element) Get() string { return text(e.el) }

// Same reports whether two slots stand for one element. Slots are made per
// call, so == does not answer that.
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

// Place moves the element to the end of its parent. AddChild moves an element
// already in the tree rather than copying it.
func (e *Element) Place() { e.parent().AddChild(e.ensure()) }

// Remove takes the element and its refinements, and unbinds the slot, so a
// later write creates a fresh element rather than reviving the detached one.
//
// ponytail: rescans the metadata per element removed. There are tens of
// elements; batch the scans if a profile says to.
func (e *Element) Remove() {
	if e.el == nil {
		return
	}
	e.d.removeRefinements([]string{e.ID()})
	detach(e.el)
	e.el = nil
}

func (e *Element) Set(value string) { e.ensure().SetText(value) }

// ID reads the element's id without minting one, since a read must not modify
// the document.
func (e *Element) ID() string {
	if e.el == nil {
		return ""
	}
	return attr(e.el, "id")
}

func (e *Element) mintID() string { return e.d.ensureID(e.ensure(), e.idPrefix) }

func (e *Element) Refine(property string) *Refine {
	return &Refine{d: e.d, owner: e, property: property}
}

func (e *Element) OPFAttr(name string) *OPFAttr {
	return &OPFAttr{d: e.d, owner: e, name: name}
}

type Refine struct {
	d             *Doc
	owner         *Element
	property      string
	unschemedOnly bool
	schemedAs     string
}

// Unschemed narrows to refinements with no scheme. D.3.4 defines series and set
// only "when no scheme is specified".
func (r *Refine) Unschemed() *Refine {
	narrowed := *r
	narrowed.unschemedOnly = true
	narrowed.schemedAs = ""
	return &narrowed
}

// Schemed resolves the scheme through the document's vocabulary.
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

// Set updates the refinement already there, so it keeps its position.
//
// ponytail: only the first of duplicate refinements is updated. Revisit if
// epubcheck rejects a file this package wrote.
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

func (r *Refine) Values() []string {
	var out []string
	for _, m := range r.elements() {
		if v := text(m); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// Add appends unconditionally, for a property whose existing value may not be
// this package's, such as a creator's second role.
func (r *Refine) Add(value, scheme string) {
	r.d.addRefine(r.owner.mintID(), r.property, value, scheme)
}

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

// Set writes the attribute Get would read. etree matches by local name, so a
// bare file-as is updated in place; adding opf:file-as beside it would leave
// two sort names. A new attribute gets the opf: prefix.
func (a *OPFAttr) Set(value string) {
	el := a.owner.ensure()
	if existing := el.SelectAttr(a.name); existing != nil {
		existing.Value = value
		return
	}
	el.CreateAttr(xml.Qualify(a.d.ns.opf(), a.name), value)
}

func (a *OPFAttr) Clear() {
	if a.owner.el == nil {
		return
	}
	if existing := a.owner.el.SelectAttr(a.name); existing != nil {
		a.owner.el.RemoveAttr(existing.FullKey())
	}
}

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

// Set updates the meta already there, so it keeps its position.
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
