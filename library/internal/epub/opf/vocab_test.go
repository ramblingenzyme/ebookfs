package opf

import (
	"strings"
	"testing"
)

// pkgWithPrefix builds a minimal package document carrying the given prefix
// attribute, for the functions that need a *Doc rather than a bare map.
func pkgWithPrefix(t *testing.T, prefix string) *Doc {
	t.Helper()
	attr := ""
	if prefix != "" {
		attr = ` prefix="` + prefix + `"`
	}
	doc, err := Parse([]byte(`<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="pub-id"` + attr + `>
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
  </metadata>
</package>`))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestExpand(t *testing.T) {
	const dcterms = "http://purl.org/dc/terms/"
	for _, tc := range []struct {
		name, prefix, in, want string
	}{
		{"unprefixed is the default vocabulary", "", "file-as", "file-as"},
		{"reserved prefix needs no declaration", "", "dcterms:modified", dcterms + "modified"},
		{"declared prefix resolves", "dct: " + dcterms, "dct:modified", dcterms + "modified"},
		{"undeclared prefix is left alone", "", "dct:modified", "dct:modified"},
		{"a declaration overrides a reserved prefix", "dcterms: http://example.com/#", "dcterms:modified", "http://example.com/#modified"},
		{"empty stays empty", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := pkgWithPrefix(t, tc.prefix)
			if got := expand(tc.in, o.vocabularies()); got != tc.want {
				t.Errorf("expand(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestVocabulariesParsesThePrefixAttribute covers D.1.4's grammar: a
// whitespace-separated list of "prefix: URL" pairs, which real files wrap.
func TestVocabulariesParsesThePrefixAttribute(t *testing.T) {
	for _, tc := range []struct {
		name, prefix string
		want         map[string]string
	}{
		{"single pair", "dct: http://purl.org/dc/terms/", map[string]string{"dct": "http://purl.org/dc/terms/"}},
		{"several pairs", "a: http://a/ b: http://b/", map[string]string{"a": "http://a/", "b": "http://b/"}},
		{"wrapped across lines", "a:\n      http://a/\n      b: http://b/", map[string]string{"a": "http://a/", "b": "http://b/"}},
		{"trailing prefix with no URL binds nothing", "a: http://a/ b:", map[string]string{"a": "http://a/", "b": ""}},
		{"entry missing its colon is skipped", "a http://a/", map[string]string{"a": ""}},
		{"absent attribute leaves the reserved set", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := pkgWithPrefix(t, tc.prefix).vocabularies()

			// The reserved set is always present and unmodified unless declared over.
			for name, url := range reservedPrefixes {
				if _, declared := tc.want[name]; declared {
					continue
				}
				if got[name] != url {
					t.Errorf("reserved %q = %q, want %q", name, got[name], url)
				}
			}
			for name, url := range tc.want {
				if url == "" {
					if _, bound := got[name]; bound {
						t.Errorf("%q was bound, want it ignored", name)
					}
					continue
				}
				if got[name] != url {
					t.Errorf("%q = %q, want %q", name, got[name], url)
				}
			}
		})
	}
}

func TestSpell(t *testing.T) {
	const dcterms = "http://purl.org/dc/terms/"
	for _, tc := range []struct {
		name, prefix, in, want string
		// declares is a binding the call is expected to add to the package.
		declares string
	}{
		{
			name: "default vocabulary is unchanged",
			in:   "belongs-to-collection", want: "belongs-to-collection",
		},
		{
			name: "reserved prefix is unchanged when nothing rebinds it",
			in:   "dcterms:modified", want: "dcterms:modified",
		},
		{
			name:   "a declaration agreeing with the reserved binding is unchanged",
			prefix: "dcterms: " + dcterms,
			in:     "dcterms:modified", want: "dcterms:modified",
		},
		{
			name:   "reuses a prefix the document already binds to the vocabulary",
			prefix: "dcterms: http://example.com/# dct: " + dcterms,
			in:     "dcterms:modified", want: "dct:modified",
		},
		{
			name:   "lowest name wins when two prefixes are bound to it",
			prefix: "dcterms: http://example.com/# zz: " + dcterms + " aa: " + dcterms,
			in:     "dcterms:modified", want: "aa:modified",
		},
		{
			name:   "declares one when the document rebound ours and offers no other",
			prefix: "dcterms: http://example.com/#",
			in:     "dcterms:modified", want: "dcterms2:modified",
			declares: "dcterms2: " + dcterms,
		},
		{
			name:   "suffixes past a taken name",
			prefix: "dcterms: http://example.com/# dcterms2: http://example.org/#",
			in:     "dcterms:modified", want: "dcterms3:modified",
			declares: "dcterms3: " + dcterms,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := pkgWithPrefix(t, tc.prefix)
			if got := o.spell(tc.in); got != tc.want {
				t.Errorf("spell(%q) = %q, want %q", tc.in, got, tc.want)
			}

			declared := attr(o.pkg, "prefix")
			if tc.declares == "" {
				if declared != tc.prefix {
					t.Errorf("prefix attribute = %q, want it untouched at %q", declared, tc.prefix)
				}
				return
			}
			if !strings.Contains(declared, tc.declares) {
				t.Errorf("prefix attribute = %q, want it to declare %q", declared, tc.declares)
			}
			if tc.prefix != "" && !strings.HasPrefix(declared, tc.prefix) {
				t.Errorf("prefix attribute = %q, want the document's own declarations kept in front", declared)
			}
		})
	}
}
