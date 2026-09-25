package opf

import (
	"slices"

	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/opf/pkgdoc"
)

type authorsField struct{ d *pkgdoc.Doc }

func (o *Doc) authors() authorsField { return authorsField{o.d} }

// creators returns the creators this package owns: those with the aut role, or
// no role. Counting a role-less creator as an author is our reading, not a
// spec rule.
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
func (f authorsField) get() []Author {
	var out []Author
	for _, c := range f.creators() {
		// §5.5.2 requires a non-empty value, so an empty creator is not an
		// author.
		name := c.Get()
		if name == "" {
			continue
		}
		sortAs := c.OPFAttr("file-as").Get() // EPUB 2
		if sortAs == "" {
			sortAs = c.Refine("file-as").Get()
		}
		out = append(out, Author{Name: name, SortName: sortAs})
	}
	return out
}

// set writes each author's name, role and sort name. reconcileCreators decides
// which elements exist.
func (f authorsField) set(authors []Author) {
	for i, c := range f.reconcileCreators(authors) {
		a := authors[i]
		c.Set(a.Name)

		if !f.d.EPUB3() {
			c.OPFAttr("role").Set("aut")
			// Put clears a sort name the author no longer has.
			pkgdoc.Put(c.OPFAttr("file-as"), a.SortName)
			continue
		}

		// D.3.10 allows zero or more roles, so aut is added only if missing and
		// other roles stay. It only SHOULDs a scheme; marc:relators is reserved
		// by D.1.5 and needs no declaration.
		if !slices.Contains(creatorRoles(c), "aut") {
			c.Refine("role").Add("aut", "marc:relators")
		}
		pkgdoc.Put(c.Refine("file-as"), a.SortName)

		if legacy := c.OPFAttr("file-as"); legacy.Get() != "" {
			pkgdoc.Put(legacy, a.SortName)
		}
	}
}

// reconcileCreators makes the document carry one creator per author, in order,
// and returns them. A creator whose name is kept is reused with its
// refinements. Other author creators are removed with theirs, and non-author
// creators are left alone.
//
// A new name gets a new element rather than a renamed old one, so refinements
// such as an alternate-script (D.3.1) never describe a name their author did
// not write.
//
// Unclaimed creators are removed only after the loop, since ensureID scans the
// tree for ids and cannot see a detached creator.
func (f authorsField) reconcileCreators(authors []Author) []*pkgdoc.Element {
	// Keyed by the name get reports, so a match is the creator the caller was
	// shown.
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

// creatorRoles returns a creator's roles in document order. An EPUB 2 opf:role
// wins when present; otherwise every EPUB 3 role refinement counts (D.3.10).
func creatorRoles(c *pkgdoc.Element) []string {
	if r := c.OPFAttr("role").Get(); r != "" {
		return []string{r}
	}
	return c.Refine("role").Values()
}
