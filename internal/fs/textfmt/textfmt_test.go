package textfmt

import "testing"

func TestParseAuthor(t *testing.T) {
	for _, tc := range []struct{ spec, name, sort string }{
		{"Ursula K. Le Guin", "Ursula K. Le Guin", ""},
		{" Ursula K. Le Guin | Le Guin, Ursula K. ", "Ursula K. Le Guin", "Le Guin, Ursula K."},
		{"Name |", "Name", ""},
		{"| Sort", "", "Sort"}, // blank name comes back for the caller to reject
		{"", "", ""},
	} {
		a := ParseAuthor(tc.spec)
		if a.Name != tc.name || a.SortName != tc.sort {
			t.Errorf("ParseAuthor(%q) = %q/%q, want %q/%q", tc.spec, a.Name, a.SortName, tc.name, tc.sort)
		}
	}
}
