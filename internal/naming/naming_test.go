package naming

import (
	"testing"
)

func TestForFAT(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"ordinary title", "The Hobbit", "The Hobbit", false},
		{"slash to hyphen", "foo/bar", "foo-bar", false},
		{"backslash to hyphen", "foo\\bar", "foo-bar", false},
		{"colon to hyphen", "Title: Subtitle", "Title- Subtitle", false},
		{"asterisk to hyphen", "foo*bar", "foo-bar", false},
		{"question mark to hyphen", "what?", "what-", false},
		{"double quote to hyphen", "\"quoted\"", "-quoted-", false},
		{"less than to hyphen", "a<b", "a-b", false},
		{"greater than to hyphen", "a>b", "a-b", false},
		{"pipe to hyphen", "a|b", "a-b", false},
		{"nul stripped", "a\x00b", "ab", false},
		{"control chars stripped", "a\x01b", "ab", false},
		{"trailing dot trimmed", "file.", "file", false},
		{"trailing space trimmed", "file ", "file", false},
		{"trailing tab trimmed", "file\t", "file", false},
		{"all combined", "Tit:le*/<>\x00. ", "Tit-le----", false},
		{"empty string", "", "", true},
		{"only stripped chars", ". \t\x01", "", true},
		{"unicode retained", "漢字:title", "漢字-title", false},
		{"only fat illegal", "\\:*?\"<>|", "--------", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ForFAT(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ForFAT(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ForFAT(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPathSafe(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"ordinary text is untouched", "The Hobbit", "The Hobbit"},
		{"slash would split a component", "Either/Or", "Either-Or"},
		{"every slash, not just the first", "a/b/c", "a-b-c"},

		{"parent directory", "..", "_"},
		{"current directory", ".", "_"},
		{"nothing but dots", "...", "_"},
		{"nothing but spaces", "   ", "_"},
		{"empty", "", "_"},
		{"slashes alone become dashes, which are a usable name", "//", "--"},

		{"trailing dot is trimmed", "Ph.D.", "Ph.D"},
		{"leading dot is trimmed", ".hidden", "hidden"},
		{"inner dots are kept", "R.U.R.", "R.U.R"},
		{"surrounding spaces are trimmed", "  Title  ", "Title"},
		{"inner spaces are kept", "A Tale", "A Tale"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := PathSafe(tc.in); got != tc.want {
				t.Errorf("PathSafe(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
