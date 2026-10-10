package opf

import (
	"slices"

	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/opf/pkgdoc"
)

// Contributor mirrors epub.Contributor.
type Contributor struct{ Name, Role string }

type contributorsField struct{ d *pkgdoc.Doc }

func (o *Doc) contributors() contributorsField { return contributorsField{o.d} }

// get returns one entry per role of each non-empty dc:contributor, in document
// order, so a person with two roles reads as two entries. A contributor with no
// role is one entry with Role "".
//
// A role repeated on one element is reported once. Files saved by an earlier
// version of set repeat it, and two equal entries would give the index two
// identical rows.
func (f contributorsField) get() []Contributor {
	out := []Contributor{}
	for _, c := range f.d.DCAll("contributor") {
		name := c.Get()
		if name == "" {
			continue
		}
		roles := elementRoles(c)
		if len(roles) == 0 {
			out = append(out, Contributor{Name: name})
		}
		for i, role := range roles {
			if !slices.Contains(roles[:i], role) {
				out = append(out, Contributor{Name: name, Role: role})
			}
		}
	}
	return out
}

// set writes contributors in order. EPUB 3 gives each person one element, with
// role refinements in the order of that person's entries, which D.3.10 makes
// their order of importance. EPUB 2 has one opf:role per element, so each name
// and role pair is its own element there.
//
// A person's existing element is reused, so refinements such as file-as
// survive a role change. Unclaimed contributors are removed with theirs.
func (f contributorsField) set(contributors []Contributor) {
	existing := f.d.DCAll("contributor")
	claimed := make([]bool, len(existing))
	claim := func(match func(*pkgdoc.Element) bool) *pkgdoc.Element {
		for j, el := range existing {
			if !claimed[j] && el.Get() != "" && match(el) {
				claimed[j] = true
				return el
			}
		}
		return nil
	}
	write := func(el *pkgdoc.Element, name string, roles []string) {
		if el == nil {
			el = f.d.NewDC("contributor")
		}
		el.Set(name)
		el.Place()
		f.setRoles(el, roles)
	}

	if f.d.EPUB3() {
		for _, p := range people(contributors) {
			write(claim(func(el *pkgdoc.Element) bool { return el.Get() == p.name }), p.name, p.roles)
		}
	} else {
		var entries []Contributor
		for _, c := range contributors {
			if !slices.Contains(entries, c) {
				entries = append(entries, c)
			}
		}
		// Exact matches are claimed first, or an entry for one role could take
		// the element a later entry for another role was shown.
		out := make([]*pkgdoc.Element, len(entries))
		for i, c := range entries {
			out[i] = claim(func(el *pkgdoc.Element) bool { return contributorOf(el) == c })
		}
		for i, c := range entries {
			if out[i] == nil {
				out[i] = claim(func(el *pkgdoc.Element) bool { return el.Get() == c.Name })
			}
			var roles []string
			if c.Role != "" {
				roles = []string{c.Role}
			}
			write(out[i], c.Name, roles)
		}
	}

	// After the writes, since ensureID scans the tree for ids and cannot see a
	// detached contributor.
	for j, el := range existing {
		if !claimed[j] {
			el.Remove()
		}
	}
}

type person struct {
	name  string
	roles []string
}

// people groups entries by name, in order of each name's first entry. An entry
// with no role adds none, so it is lost beside an entry for the same name that
// has one.
func people(contributors []Contributor) []person {
	var out []person
	at := map[string]int{}
	for _, c := range contributors {
		i, seen := at[c.Name]
		if !seen {
			i = len(out)
			at[c.Name] = i
			out = append(out, person{name: c.Name})
		}
		if c.Role != "" && !slices.Contains(out[i].roles, c.Role) {
			out[i].roles = append(out[i].roles, c.Role)
		}
	}
	return out
}

// contributorOf is the one entry an EPUB 2 element reports, its opf:role
// holding a single value.
func contributorOf(c *pkgdoc.Element) Contributor {
	role := ""
	if roles := elementRoles(c); len(roles) > 0 {
		role = roles[0]
	}
	return Contributor{Name: c.Get(), Role: role}
}

// setRoles makes roles what get reads from el. EPUB 3 keeps them in role
// refinements. A legacy opf:role outranks those on read and holds one value, so
// it is kept in step for one role and removed for more.
func (f contributorsField) setRoles(el *pkgdoc.Element, roles []string) {
	legacy := el.OPFAttr("role")
	if !f.d.EPUB3() {
		role := ""
		if len(roles) > 0 {
			role = roles[0]
		}
		pkgdoc.Put(legacy, role)
		return
	}
	if legacy.Get() != "" {
		if len(roles) == 1 {
			legacy.Set(roles[0])
		} else {
			legacy.Clear()
		}
	}
	// D.3.10 only SHOULDs a scheme. marc:relators is reserved by D.1.5 and needs
	// no declaration.
	el.Refine("role").SetValues(roles, "marc:relators")
}
