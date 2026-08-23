package opf

import "testing"

// TestDCPrefix covers the fallback that has no path through Parse: a package
// with no dc element to copy a prefix from. Reachable only while creating the
// first one, so the whole branch rests on getting it right without a corpus to
// catch it.
//
// The bug it replaced was returning "dc" unconditionally, which puts a new
// element in no namespace at all when the document binds Dublin Core to some
// other prefix, or to none.
func TestDCPrefix(t *testing.T) {
	for _, tc := range []struct {
		name, pkgAttrs, metadata, want string
		// declares is an xmlns: binding the call is expected to add.
		declares string
	}{
		{
			name:     "copies the prefix an existing dc element uses",
			metadata: `<dc:title>T</dc:title>`,
			want:     "dc",
		},
		{
			name:     "copies an unusual prefix rather than assuming dc",
			pkgAttrs: ` xmlns:dcx="http://purl.org/dc/elements/1.1/"`,
			metadata: `<dcx:title>T</dcx:title>`,
			want:     "dcx",
		},
		{
			name:     "no dc element: takes the prefix the document declares",
			pkgAttrs: ` xmlns:dcx="http://purl.org/dc/elements/1.1/"`,
			want:     "dcx",
		},
		{
			name: "no dc element and no declaration: declares one",
			want: "dc", declares: "http://purl.org/dc/elements/1.1/",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := Parse([]byte(`<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="pub-id"` + tc.pkgAttrs + `>
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    ` + tc.metadata + `
  </metadata>
</package>`))
			if err != nil {
				t.Fatal(err)
			}

			got := o.dcPrefix()
			if got != tc.want {
				t.Errorf("dcPrefix() = %q, want %q", got, tc.want)
			}
			// Whatever it returns must actually be bound, or a new element lands
			// in no namespace.
			if bound := o.pkg.SelectAttrValue("xmlns:"+got, ""); tc.declares != "" && bound != tc.declares {
				t.Errorf("xmlns:%s = %q, want %q declared", got, bound, tc.declares)
			}
		})
	}
}
