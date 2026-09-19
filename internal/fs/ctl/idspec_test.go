package ctl

import (
	"reflect"
	"slices"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func TestParseSelection(t *testing.T) {
	tests := []struct {
		spec    string
		all     bool // "*" resolves to an empty Query (every book)
		wantIDs []int64
		wantErr bool
	}{
		{"*", true, nil, false},
		{"1", false, []int64{1}, false},
		{"1,2,3", false, []int64{1, 2, 3}, false},
		{"1, 2, 3", false, []int64{1, 2, 3}, false},
		{"10", false, []int64{10}, false},
		{"", false, nil, true},
		{"abc", false, nil, true},
		{"1,abc", false, nil, true}, // no colon, so it stays an id-spec error
	}

	for _, tt := range tests {
		got, err := parseSelection(tt.spec)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseSelection(%q) error = %v, wantErr = %v", tt.spec, err, tt.wantErr)
			continue
		}
		if tt.wantErr {
			continue
		}
		if tt.all {
			if len(got.IDs) != 0 {
				t.Errorf("parseSelection(%q) IDs = %v, want empty (all books)", tt.spec, got.IDs)
			}
			continue
		}
		if !slices.Equal(got.IDs, tt.wantIDs) {
			t.Errorf("parseSelection(%q) IDs = %v, want %v", tt.spec, got.IDs, tt.wantIDs)
		}
	}
}

// parseSelection falls through to the shared query parser, so a ctl command can
// select by metadata instead of by id.
func TestParseSelectionQuerySyntax(t *testing.T) {
	got, err := parseSelection("tag:sci-fi+status:unread")
	if err != nil {
		t.Fatalf("parseSelection: %v", err)
	}
	// ExactTitles: a ctl selection mutates books, so title: must not match
	// substrings the way the search view does.
	want := library.Query{Tags: []string{"sci-fi"}, Status: []string{"unread"}, ExactTitles: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseSelection = %+v, want %+v", got, want)
	}
}
