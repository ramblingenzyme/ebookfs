package opf

import (
	"slices"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/internal/naming"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

type authorsField struct{ o *Doc }

func (o *Doc) authors() authorsField { return authorsField{o} }

// creators returns the creator elements this package owns — those
// isAuthorCreator claims. Non-author contributors (editors, illustrators,
// translators) are parsed correctly but deliberately excluded: the frontend has
// no concept of contributor roles, so exposing them would create a broken
// round-trip where removing an editor from the 9P authors field appears to work
// but the editor survives in the epub.
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
// order: "The document order of dc:creator elements in the metadata section
// determines the display priority."
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

// set replaces the author creators and leaves every other creator alone.
//
// Re-adding each creator in the order given is what puts document order in
// agreement with the order the edit asked for, which get reads back: etree's
// AddChild detaches an element from its current parent first, so adding one
// that is already in the tree moves it to the end rather than duplicating it.
// A creator whose name survives is reused rather than rebuilt, so refinements
// this package does not manage — alternate-script, alternate display names,
// third-party metadata — keep pointing at a live creator; only the role and the
// sort name are ours to write. Creators that did not survive are removed at the
// end, with their refinements.
//
// Nothing is detached before the loop, and that is load-bearing: ensureID mints
// against the ids present in the tree, so a creator sitting outside it is
// invisible and its id can be minted on top of. An earlier version detached
// every creator up front and did exactly that — editing ["Alice"] to
// ["Bob", "Alice"] gave both creators the id "ebookfs-creator", and Bob's sort
// name was written to Alice's refinement and then overwritten by it.
func (f authorsField) set(authors []model.Author) {
	o := f.o

	// Keyed by the same name get reports, so a creator this matches is the one
	// the caller was shown. A creator with no usable name — or a second sharing
	// one — is left unmatchable, and is rebuilt from scratch.
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
		if reused {
			// Handed out once: a name repeated in the new list gets a fresh
			// creator the second time rather than the same element twice.
			delete(byName, a.Name)
		} else {
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
		// D.3.10 gives role cardinality "zero or more", and Example 92 gives one
		// dc:creator both aut and ill — so the aut relator is added only when
		// absent, and any other role the creator carries is left alone.
		// Rewriting the roles wholesale deleted an illustrator credit on the
		// first edit of any kind.
		if !slices.Contains(creatorRoles(o, c), "aut") {
			o.addRefine(id, propRole, "aut", "marc:relators")
		}
		if a.SortName != "" {
			o.setRefine(id, propFileAs, a.SortName, "")
		} else {
			o.removeRefine(id, propFileAs)
		}
	}

	// Refinements follow their creator: one that survived keeps everything we
	// did not rewrite, one that did not loses them all.
	var dropped []string
	for c := range unclaimed {
		dropped = append(dropped, attr(c, "id"))
		detach(c)
	}
	o.removeMetas(func(m *etree.Element) bool { return refinesAny(m, dropped) })
}

// creatorRoles returns the roles attached to a creator, in document order. The
// EPUB 2 opf:role attribute is a single value and wins outright when present;
// otherwise the EPUB 3 role refinements are used, all of them — D.3.10 gives
// role cardinality "zero or more", and Example 92 gives one dc:creator both
// aut and ill, with the most important role first.
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
// Both the reader (translateAuthor) and the writer (setAuthors) go through it,
// so a creator setAuthors replaces is exactly one translate reports.
//
// Treating an unspecified role as author is our interpretation, not a rule
// either spec states: EPUB 3.3 §5.5.3.2.3 makes the role property optional and
// describes dc:creator as the party "responsible for the creation of the
// content"; OPF 2.0 §2.2.2 calls it "A primary creator or author of the
// publication". Neither declares a default.
func isAuthorCreator(o *Doc, c *etree.Element) bool {
	roles := creatorRoles(o, c)
	return len(roles) == 0 || slices.Contains(roles, "aut")
}
