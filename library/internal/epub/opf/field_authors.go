package opf

import (
	"slices"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/internal/naming"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

type authorsField struct{ o *Doc }

func (o *Doc) authors() authorsField { return authorsField{o} }

// creators returns the creator elements this package owns. Other contributors
// (editors, illustrators, translators) are excluded on purpose.
func (f authorsField) creators() []*etree.Element {
	var out []*etree.Element
	for _, c := range f.o.elements("creator") {
		if isAuthorCreator(f.o, c) {
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
		name, err := naming.Sanitize(text(c))
		if err != nil {
			continue
		}
		sortAs := attr(c, "file-as") // EPUB 2
		if sortAs == "" {
			sortAs = f.o.refine(attr(c, "id"), propFileAs)
		}
		if sortAs != "" {
			sortAs, _ = naming.Sanitize(sortAs)
		}
		out = append(out, model.Author{Name: name, SortName: sortAs})
	}
	return out
}

// set replaces the author creators and leaves every other creator alone. A
// creator whose name survives is reused, so refinements this package does not
// manage stay attached to it; only the role and the sort name are ours to write.
// AddChild moves an element already in the tree rather than duplicating it.
//
// Do not detach creators before the loop. ensureID mints ids by scanning the
// tree, so a detached creator is invisible to it and a later creator can be
// given the same id.
func (f authorsField) set(authors []model.Author) {
	o := f.o

	// Keyed by the name get reports, so a match is the creator the caller was
	// shown. Anything unmatchable is rebuilt from scratch.
	byName := map[string]*etree.Element{}
	unclaimed := map[*etree.Element]bool{}
	for _, c := range f.creators() {
		unclaimed[c] = true
		if name, err := naming.Sanitize(text(c)); err == nil {
			if _, seen := byName[name]; !seen {
				byName[name] = c
			}
		}
	}

	home := o.dcHome()
	for _, a := range authors {
		c, reused := byName[a.Name]
		if !reused {
			c = etree.NewElement(qualify(o.dcPrefix(), "creator"))
		}
		home.AddChild(c)
		delete(unclaimed, c)
		c.SetText(a.Name)

		if !o.epub3() {
			prefix := o.ensureOPFPrefix()
			c.CreateAttr(qualify(prefix, "role"), "aut")
			// The author may have lost the sort name it was written with.
			c.RemoveAttr(qualify(prefix, "file-as"))
			if a.SortName != "" {
				c.CreateAttr(qualify(prefix, "file-as"), a.SortName)
			}
			continue
		}

		id := o.ensureID(c, "ebookfs-creator")
		// Roles are "zero or more" (D.3.10), so aut is added only when absent
		// and any other role is left alone.
		if !slices.Contains(creatorRoles(o, c), "aut") {
			o.addRefine(id, propRole, "aut", "marc:relators")
		}
		if a.SortName != "" {
			o.setRefine(id, propFileAs, a.SortName, "")
		} else {
			o.removeRefine(id, propFileAs)
		}
	}

	// Refinements follow their creator.
	var dropped []string
	for c := range unclaimed {
		dropped = append(dropped, attr(c, "id"))
		detach(c)
	}
	o.removeMetas(func(m *etree.Element) bool { return refinesAny(m, dropped) })
}

// creatorRoles returns the roles attached to a creator, in document order. The
// single-valued EPUB 2 opf:role wins outright when present; otherwise all of the
// EPUB 3 role refinements are used (D.3.10, "zero or more").
func creatorRoles(o *Doc, c *etree.Element) []string {
	if r := attr(c, "role"); r != "" {
		return []string{r}
	}
	var roles []string
	for _, m := range o.refinements(attr(c, "id"), propRole) {
		if v := text(m); v != "" {
			roles = append(roles, v)
		}
	}
	return roles
}

// isAuthorCreator is the single definition of which creator counts as an
// author: one carrying the "aut" MARC relator, or one carrying no role at all.
// The second half is our interpretation, not a rule either spec states.
func isAuthorCreator(o *Doc, c *etree.Element) bool {
	roles := creatorRoles(o, c)
	return len(roles) == 0 || slices.Contains(roles, "aut")
}
