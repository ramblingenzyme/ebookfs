package ctl

import (
	"slices"
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		input   string
		name    string
		args    []string
		wantErr bool
	}{
		{"add-tag sci-fi 1,2,3", "add-tag", []string{"sci-fi", "1,2,3"}, false},
		{"delete 42", "delete", []string{"42"}, false},
		{`add-tag "science fiction" 1`, "add-tag", []string{"science fiction", "1"}, false},
		{`rename-author "Asimov" "Isaac Asimov|Asimov, Isaac"`, "rename-author", []string{"Asimov", "Isaac Asimov|Asimov, Isaac"}, false},
		{`add-tag "foo 1`, "", nil, true},
		{"", "", nil, true},
	}

	for _, tt := range tests {
		name, args, err := parseCommand(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseCommand(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
			continue
		}
		if name != tt.name {
			t.Errorf("parseCommand(%q).name = %q, want %q", tt.input, name, tt.name)
		}
		if !slices.Equal(args, tt.args) {
			t.Errorf("parseCommand(%q).args = %q, want %q", tt.input, args, tt.args)
		}
	}
}
