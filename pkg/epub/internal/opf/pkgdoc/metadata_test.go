package pkgdoc

import "testing"

// No fixture reaches the no-dc-element fallback, since it runs only while
// creating the first one. Regression: it returned "dc" unconditionally, which
// put a new element in no namespace when Dublin Core was bound elsewhere.
func TestDCPrefix(t *testing.T) {
	for _, tc := range []struct {
		name, pkgAttrs, metadata, want string
		declares                       string
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
			d, err := Parse([]byte(`<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="pub-id"` + tc.pkgAttrs + `>
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    ` + tc.metadata + `
  </metadata>
</package>`))
			if err != nil {
				t.Fatal(err)
			}

			got := d.dcPrefix()
			if got != tc.want {
				t.Errorf("dcPrefix() = %q, want %q", got, tc.want)
			}
			if bound := d.pkg.SelectAttrValue("xmlns:"+got, ""); tc.declares != "" && bound != tc.declares {
				t.Errorf("xmlns:%s = %q, want %q declared", got, bound, tc.declares)
			}
		})
	}
}
