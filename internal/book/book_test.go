package book

import (
	"testing"
)

func TestJoinAuthors(t *testing.T) {
	tests := []struct {
		name    string
		authors []Author
		sep     string
		want    string
	}{
		{"single", []Author{{Name: "Alice"}}, ", ", "Alice"},
		{"multiple", []Author{{Name: "Alice"}, {Name: "Bob"}}, ", ", "Alice, Bob"},
		{"directory separator", []Author{{Name: "Alice"}, {Name: "Bob"}}, " & ", "Alice & Bob"},
		{"empty names", []Author{{Name: ""}, {Name: ""}}, ", ", UnknownAuthor},
		{"mixed empty and valid", []Author{{Name: ""}, {Name: "Alice"}}, ", ", "Alice"},
		{"nil", nil, ", ", UnknownAuthor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := JoinAuthors(tt.authors, tt.sep)
			if got != tt.want {
				t.Errorf("JoinAuthors(%v, %q) = %q, want %q", tt.authors, tt.sep, got, tt.want)
			}
		})
	}
}
