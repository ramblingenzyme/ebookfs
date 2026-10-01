package opf

import (
	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/opf/pkgdoc"
)

// Contributor mirrors epub.Contributor.
type Contributor struct{ Name, Role string }

type contributorsField struct{ d *pkgdoc.Doc }

func (o *Doc) contributors() contributorsField { return contributorsField{o.d} }

// contributors returns all non-empty dc:contributor elements in document order,
// with their roles. A contributor with no role has Role = "".
func (f contributorsField) get() []Contributor {
	out := []Contributor{}
	for _, c := range f.d.DCAll("contributor") {
		name := c.Get()
		if name == "" {
			continue
		}
		roles := elementRoles(c)
		role := ""
		if len(roles) > 0 {
			role = roles[0]
		}
		out = append(out, Contributor{Name: name, Role: role})
	}
	return out
}

// set reconciles contributors by name: elements with matching names are reused
// (preserving their role and file-as refinements), unmatched elements are removed,
// and new contributors get new elements.
func (f contributorsField) set(contributors []Contributor) {
	byName := map[string]*pkgdoc.Element{}
	unclaimed := map[*pkgdoc.Element]bool{}
	for _, c := range f.d.DCAll("contributor") {
		if name := c.Get(); name != "" {
			if _, seen := byName[name]; !seen {
				byName[name] = c
			}
		}
		unclaimed[c] = true
	}

	for _, contrib := range contributors {
		el, reused := byName[contrib.Name]
		if !reused {
			el = f.d.NewDC("contributor")
		}
		el.Set(contrib.Name)
		el.Place()
		delete(unclaimed, el)

		// Write the role. EPUB 2 uses opf:role attribute; EPUB 3 uses a refinement.
		if !f.d.EPUB3() {
			if contrib.Role != "" {
				el.OPFAttr("role").Set(contrib.Role)
			} else {
				el.OPFAttr("role").Clear()
			}
			continue
		}

		// EPUB 3: write role as a refinement.
		roles := el.Refine("role")
		if contrib.Role != "" {
			// D.3.10 allows zero or more roles. We write one.
			roles.Add(contrib.Role, "marc:relators")
		} else {
			roles.Clear()
		}
	}

	for el := range unclaimed {
		el.Remove()
	}
}
