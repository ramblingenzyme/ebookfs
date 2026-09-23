package naming

import (
	"testing"
)

func TestForFAT(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{"ordinary title", "The Hobbit", "The Hobbit"},
		{"slash to hyphen", "foo/bar", "foo-bar"},
		{"backslash to hyphen", "foo\\bar", "foo-bar"},
		{"colon to hyphen", "Title: Subtitle", "Title- Subtitle"},
		{"asterisk to hyphen", "foo*bar", "foo-bar"},
		{"question mark to hyphen", "what?", "what-"},
		{"double quote to hyphen", "\"quoted\"", "-quoted-"},
		{"less than to hyphen", "a<b", "a-b"},
		{"greater than to hyphen", "a>b", "a-b"},
		{"pipe to hyphen", "a|b", "a-b"},
		{"nul stripped", "a\x00b", "ab"},
		{"control chars stripped", "a\x01b", "ab"},
		{"trailing dot trimmed", "file.", "file"},
		{"trailing space trimmed", "file ", "file"},
		{"trailing tab trimmed", "file\t", "file"},
		{"all combined", "Tit:le*/<>\x00. ", "Tit-le----"},
		// Nothing survives sanitizing, so the input comes back unchanged.
		{"empty string", "", ""},
		{"only stripped chars", ". \t\x01", ". \t\x01"},
		{"unicode retained", "漢字:title", "漢字-title"},
		{"only fat illegal", "\\:*?\"<>|", "--------"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ForFAT(tt.input); got != tt.want {
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

		// POSIX forbids NUL in a filename. It forbids nothing else below 0x20,
		// so those stay: dropping them would lose part of a name the
		// destination would have accepted.
		{"nul is dropped", "a\x00b", "ab"},
		{"other control characters are kept", "a\x01b", "a\x01b"},
		{"nul alone names nothing", "\x00", "_"},

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

// NamesNothing answers, ahead of the call, which inputs PathSafe can only give
// its placeholder. The two must agree: an input NamesNothing accepts has to
// come back from PathSafe as something other than "_", or a caller refusing on
// one and filing on the other would disagree with itself.
func TestNamesNothingAgreesWithPathSafe(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		want     bool
	}{
		{"parent directory", "..", true},
		{"current directory", ".", true},
		{"nothing but dots", "...", true},
		{"nothing but spaces", "   ", true},
		{"tab", "\t", true},
		{"empty", "", true},
		{"nul alone", "\x00", true},

		{"ordinary text", "The Hobbit", false},
		{"leading dot leaves a name", ".hidden", false},
		{"slashes become dashes, which are a usable name", "//", false},
		{"underscore is an ordinary name", "_", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := NamesNothing(tc.in)
			if got != tc.want {
				t.Errorf("NamesNothing(%q) = %v, want %v", tc.in, got, tc.want)
			}
			// "_" is the one input that is safe already and equals the
			// placeholder, so it is excluded from the agreement check.
			if safe := PathSafe(tc.in); tc.in != "_" && (safe == "_") != got {
				t.Errorf("PathSafe(%q) = %q but NamesNothing = %v", tc.in, safe, got)
			}
		})
	}
}
