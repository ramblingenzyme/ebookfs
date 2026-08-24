package opf

import (
	"slices"

	"github.com/ramblingenzyme/ebookfs/library/internal/epub/opf/pkgdoc"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

type authorsField struct{ d *pkgdoc.Doc }

func (o *Doc) authors() authorsField { return authorsField{o.d} }

// creators returns the creator elements this package owns: those carrying the
// "aut" MARC relator, or carrying no role at all. That second half is our
// interpretation, not a rule either spec states. Other contributors (editors,
// illustrators, translators) are excluded on purpose.
func (f authorsField) creators() []*pkgdoc.Element {
	var out []*pkgdoc.Element
	for _, c := range f.d.DCAll("creator") {
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
		name := c.Get()
		if name == "" {
			continue
		}
		sortAs := c.OPFAttr("file-as").Get() // EPUB 2
		if sortAs == "" {
			sortAs = c.Refine("file-as").Get()
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
		c.Set(a.Name)

		if !f.d.EPUB3() {
			c.OPFAttr("role").Set("aut")
			// The author may have lost the sort name it was written with.
			pkgdoc.Put(c.OPFAttr("file-as"), a.SortName)
			continue
		}

		// Roles are "zero or more" (D.3.10), so aut is added only when absent
		// and any other role is left alone. D.3.10 only SHOULDs a scheme, and
		// names no particular one; marc:relators is our choice, reserved by
		// D.1.5 so it needs no declaration.
		if !slices.Contains(creatorRoles(c), "aut") {
			c.Refine("role").Add("aut", "marc:relators")
		}
		pkgdoc.Put(c.Refine("file-as"), a.SortName)

		if legacy := c.OPFAttr("file-as"); legacy.Get() != "" {
			pkgdoc.Put(legacy, a.SortName)
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
func (f authorsField) reconcileCreators(authors []model.Author) []*pkgdoc.Element {
	// Keyed by the name get reports, so a match is the creator the caller was
	// shown. Anything unmatchable is rebuilt from scratch.
	byName := map[string]*pkgdoc.Element{}
	unclaimed := map[*pkgdoc.Element]bool{}
	for _, c := range f.creators() {
		unclaimed[c] = true
		if name := c.Get(); name != "" {
			if _, seen := byName[name]; !seen {
				byName[name] = c
			}
		}
	}

	out := make([]*pkgdoc.Element, len(authors))
	for i, a := range authors {
		c, reused := byName[a.Name]
		if !reused {
			c = f.d.NewDC("creator")
		}
		c.Place()
		delete(unclaimed, c)
		out[i] = c
	}

	for c := range unclaimed {
		c.Remove()
	}
	return out
}

// creatorRoles returns the roles attached to a creator, in document order. The
// single-valued EPUB 2 opf:role wins outright when present; otherwise all of the
// EPUB 3 role refinements are used (D.3.10, "zero or more").
func creatorRoles(c *pkgdoc.Element) []string {
	if r := c.OPFAttr("role").Get(); r != "" {
		return []string{r}
	}
	return c.Refine("role").Values()
}
