package opf

import (
	"slices"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

type authorsField struct{ o *Doc }

func (o *Doc) authors() authorsField { return authorsField{o} }

// creatorSlot wraps a creator element. create leaves the new element detached:
// reconcileCreators places every creator itself, in author order.
func (f authorsField) creatorSlot(el *etree.Element) *elementSlot {
	o := f.o
	return &elementSlot{
		o:        o,
		el:       el,
		idPrefix: "ebookfs-creator",
		create:   func() *etree.Element { return etree.NewElement(qualify(o.dcPrefix(), "creator")) },
	}
}

// creators returns the creator elements this package owns: those carrying the
// "aut" MARC relator, or carrying no role at all. That second half is our
// interpretation, not a rule either spec states. Other contributors (editors,
// illustrators, translators) are excluded on purpose.
func (f authorsField) creators() []*elementSlot {
	var out []*elementSlot
	for _, el := range f.o.elements("creator") {
		c := f.creatorSlot(el)
		roles := creatorRoles(c)
		if len(roles) == 0 || slices.Contains(roles, "aut") {
			out = append(out, c)
		}
	}
	return out
}

// get returns the authors in document order, which §5.5.3.2.3 makes the display
// order.
func (f authorsField) get() []model.Author {
	var out []model.Author
	for _, c := range f.creators() {
		// Reported as written; §5.5.2 requires a non-empty value, so a creator
		// with none is not an author. Making a name safe to use as a path
		// component belongs to whoever builds the path, not here.
		name := c.get()
		if name == "" {
			continue
		}
		sortAs := c.opfAttr("file-as").get() // EPUB 2
		if sortAs == "" {
			sortAs = c.refine("file-as").get()
		}
		out = append(out, model.Author{Name: name, SortName: sortAs})
	}
	return out
}

// set writes each author's name, role and sort name. Which creator elements
// exist is reconcileCreators' business; only the values are written here.
func (f authorsField) set(authors []model.Author) {
	for i, c := range f.reconcileCreators(authors) {
		a := authors[i]
		c.set(a.Name)

		if !f.o.epub3() {
			c.opfAttr("role").set("aut")
			// The author may have lost the sort name it was written with.
			put(c.opfAttr("file-as"), a.SortName)
			continue
		}

		// Roles are "zero or more" (D.3.10), so aut is added only when absent
		// and any other role is left alone.
		if !slices.Contains(creatorRoles(c), "aut") {
			c.refine("role").add("aut", "marc:relators")
		}
		put(c.refine("file-as"), a.SortName)

		if legacy := c.opfAttr("file-as"); legacy.get() != "" {
			put(legacy, a.SortName)
		}
	}
}

// reconcileCreators makes the document carry one creator element per author, in
// order, and returns them. A creator whose name survives is reused, so
// refinements this package does not manage stay attached to it. Every other
// author creator is dropped along with its refinements; creators that are not
// authors are left alone throughout.
//
// Do not detach the unclaimed creators before the loop finishes. ensureID mints
// ids by scanning the tree, so a detached creator is invisible to it and a later
// creator could be given the same id.
func (f authorsField) reconcileCreators(authors []model.Author) []*elementSlot {
	// Keyed by the name get reports, so a match is the creator the caller was
	// shown. Anything unmatchable is rebuilt from scratch.
	byName := map[string]*elementSlot{}
	unclaimed := map[*elementSlot]bool{}
	for _, c := range f.creators() {
		unclaimed[c] = true
		if name := c.get(); name != "" {
			if _, seen := byName[name]; !seen {
				byName[name] = c
			}
		}
	}

	parent := f.o.dcParent()
	out := make([]*elementSlot, len(authors))
	for i, a := range authors {
		c, reused := byName[a.Name]
		if !reused {
			c = f.creatorSlot(nil)
		}
		// AddChild moves an element already in the tree rather than duplicating it.
		parent.AddChild(c.ensure())
		delete(unclaimed, c)
		out[i] = c
	}

	// Refinements follow their creator.
	var dropped []string
	for c := range unclaimed {
		dropped = append(dropped, attr(c.el, "id"))
		detach(c.el)
	}
	f.o.removeRefinements(dropped)
	return out
}

// creatorRoles returns the roles attached to a creator, in document order. The
// single-valued EPUB 2 opf:role wins outright when present; otherwise all of the
// EPUB 3 role refinements are used (D.3.10, "zero or more").
func creatorRoles(c *elementSlot) []string {
	if r := c.opfAttr("role").get(); r != "" {
		return []string{r}
	}
	return c.refine("role").values()
}
