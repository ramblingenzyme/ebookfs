package pdf

import (
	"encoding/xml"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/ramblingenzyme/ebookfs/pkg/pdf/internal/cos"
	"github.com/ramblingenzyme/ebookfs/pkg/pdf/internal/xmp"
)

// store is one of the places a PDF keeps metadata: the information
// dictionary, the catalog, or the XMP packet. Open reads each field from the
// stores that keep it, and Save rewrites every store that disagrees with the
// Book.
type store interface {
	// keeps reports whether f lives in this store.
	keeps(f field) bool
	// value returns what the store holds for f, and false if it holds nothing.
	value(f field) ([]string, bool)
	// form is v as the store would hold it, so two values compare equal when
	// writing one over the other would change nothing.
	form(v []string) []string
}

// stale returns the fields st keeps whose value disagrees with m's.
func stale(st store, m *Metadata) []field {
	var out []field

	for _, f := range fields {
		have, _ := st.value(f)
		if st.keeps(f) && !slices.Equal(st.form(f.get(m)), st.form(have)) {
			out = append(out, f)
		}
	}

	return out
}

// dictStore is a dictionary of text strings: the information dictionary or
// the catalog.
type dictStore struct {
	kind dictKind
	d    cos.Dict
}

func (s dictStore) keeps(f field) bool { return f.dict == s.kind }

// value reads a text string entry. A value of another type, which §14.3.3
// does not allow, reads as absent.
func (s dictStore) value(f field) ([]string, bool) {
	v, ok := s.d[f.key].(cos.String)
	if !s.keeps(f) || !ok {
		return nil, false
	}

	return []string{cos.Text(v)}, true
}

// form joins several values, since an entry holds one string. The dictionary's
// Author is the only field with several.
func (dictStore) form(v []string) []string {
	if s := strings.Join(v, authorSeparator); s != "" {
		return []string{s}
	}

	return nil
}

// apply returns a copy of the dictionary holding m's values, and whether any
// entry changed.
func (s dictStore) apply(m *Metadata) (cos.Dict, bool) {
	d := cos.Dict{}
	maps.Copy(d, s.d)

	moved := stale(s, m)
	for _, f := range moved {
		if want := f.get(m); len(want) == 0 {
			delete(d, f.key)
		} else {
			d[f.key] = cos.TextString(strings.Join(want, authorSeparator))
		}
	}

	return d, len(moved) > 0
}

// xmpStore is the XMP packet, nil if the file has none.
type xmpStore struct{ p *xmp.Packet }

// keeps is true for every field, since XMP has a property for each.
func (xmpStore) keeps(field) bool { return true }

// value reads only the first item of a single-valued field, so a write leaves
// an array's later items alone while the first agrees. A qualified field reads
// as its value and that qualifier.
func (s xmpStore) value(f field) ([]string, bool) {
	if f.qual != (xml.Name{}) {
		items, ok := s.p.Items(f.prop)
		if len(items) == 0 {
			return nil, ok
		}

		return []string{items[0].Value, items[0].Qualifiers[f.qual]}, ok
	}

	v, ok := s.p.Values(f.prop)
	if f.single && len(v) > 1 {
		v = v[:1]
	}

	return v, ok
}

func (xmpStore) form(v []string) []string { return v }

// properties returns the edit that brings the packet up to date with m,
// refusing a value XML 1.0 cannot represent.
func (s xmpStore) properties(m *Metadata) ([]xmp.Property, error) {
	var props []xmp.Property

	for _, f := range stale(s, m) {
		want := f.get(m)

		for _, v := range want {
			if !xmp.Representable(v) {
				return nil, fmt.Errorf("refusing to edit: %s %q holds a character XML 1.0 cannot represent", f.name, v)
			}
		}

		props = append(props, f.property(want))
	}

	return props, nil
}

// property is the XMP edit that writes v as f's value.
func (f field) property(v []string) xmp.Property {
	p := xmp.Property{Name: f.prop, Form: f.form, Values: v}

	if f.qual != (xml.Name{}) && len(v) == 2 {
		p.Values, p.Qualifiers = v[:1], map[xml.Name]string{}
		if v[1] != "" {
			p.Qualifiers[f.qual] = v[1]
		}
	}

	return p
}
