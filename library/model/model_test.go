package model

import (
	"testing"
)

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
