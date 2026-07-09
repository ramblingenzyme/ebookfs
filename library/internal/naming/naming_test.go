package naming

import (
	"testing"
)

func TestSanitize(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"ordinary title", "The Hobbit", "The Hobbit", false},
		{"slash to hyphen", "foo/bar", "foo-bar", false},
		{"nul stripped", "a\x00b", "ab", false},
		{"control chars stripped", "a\x01b\x1Fc", "abc", false},
		{"leading dot trimmed", ".hidden", "hidden", false},
		{"trailing dot trimmed", "file.", "file", false},
		{"leading space trimmed", "  title", "title", false},
		{"trailing space trimmed", "title  ", "title", false},
		{"leading tab trimmed", "\ttitle", "title", false},
		{"trailing tab trimmed", "title\t", "title", false},
		{"all combined", "/a\x00b.", "-ab", false},
		{"empty string", "", "", true},
		{"only stripped chars", "\x01 \t.", "", true},
		{"only slashes", "///", "---", false},
		{"unicode retained", "漢字 title", "漢字 title", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Sanitize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Sanitize(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

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
