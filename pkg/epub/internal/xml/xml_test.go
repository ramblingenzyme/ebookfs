package xml

import "testing"

func TestResolveHref(t *testing.T) {
	for _, tc := range []struct {
		name, base, href, want string
	}{
		{"relative", "OEBPS", "cover.jpg", "OEBPS/cover.jpg"},
		{"root dir", ".", "cover.jpg", "cover.jpg"},
		{"nested base", "OEBPS/images", "cover.jpg", "OEBPS/images/cover.jpg"},
		{"parent traversal", "OEBPS/text", "../images/cover.jpg", "OEBPS/images/cover.jpg"},
		{"container-absolute", "OEBPS", "/images/cover.jpg", "images/cover.jpg"},
		{"percent-encoded space", "OEBPS", "cover%20image.jpg", "OEBPS/cover image.jpg"},
		{"percent-encoded utf8", "OEBPS", "couv%C3%A9.jpg", "OEBPS/couvé.jpg"},
		{"absolute and encoded", "OEBPS", "/img/a%20b.jpg", "img/a b.jpg"},
		{"fragment dropped", "OEBPS", "cover.xhtml#cover-image", "OEBPS/cover.xhtml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveHref(tc.base, tc.href); got != tc.want {
				t.Errorf("ResolveHref(%q, %q) = %q, want %q", tc.base, tc.href, got, tc.want)
			}
		})
	}
}

// XML 1.0 §2.2: Char ::= #x9 | #xA | #xD | [#x20-#xD7FF] | [#xE000-#xFFFD] |
// [#x10000-#x10FFFF].
func TestRepresentable(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    string
		want bool
	}{
		{"plain", "Dune", true},
		{"tab newline return", "a\tb\nc\rd", true},
		{"astral", "📚", true},
		{"NUL", "a\x00b", false},
		{"other C0 control", "a\x01b", false},
		{"noncharacter FFFE", "a￾b", false},
		{"invalid UTF-8", "a\xffb", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Representable(tc.s); got != tc.want {
				t.Errorf("Representable(%q) = %v, want %v", tc.s, got, tc.want)
			}
		})
	}
}
