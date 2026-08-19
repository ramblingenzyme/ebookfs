package opf

import "testing"

func TestCoverUrl(t *testing.T) {
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := coverUrl(tc.base, tc.href); got != tc.want {
				t.Errorf("coverUrl(%q, %q) = %q, want %q", tc.base, tc.href, got, tc.want)
			}
		})
	}
}
