package ctl

import (
	"reflect"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func TestParseSelection(t *testing.T) {
	tests := []struct {
		spec    string
		want    library.Query
		wantErr bool
	}{
		{"*", library.Query{}, false},
		{"1", library.Query{IDs: []int64{1}}, false},
		{"1 2 3", library.Query{IDs: []int64{1, 2, 3}}, false},
		{"1,2,3", library.Query{IDs: []int64{1, 2, 3}}, false},
		{"1\t2, 3", library.Query{IDs: []int64{1, 2, 3}}, false},
		{"10", library.Query{IDs: []int64{10}}, false},
		{"", library.Query{}, true},
		{"abc", library.Query{}, true},
		{"1,abc", library.Query{}, true},
		{"tag:sci-fi+status:unread", library.Query{Tags: []string{"sci-fi"}, Status: []string{"unread"}, ExactTitles: true}, false},
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, err := parseSelection(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSelection(%q) error = %v, wantErr = %v", tt.spec, err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseSelection(%q) = %+v, want %+v", tt.spec, got, tt.want)
			}
		})
	}
}

func TestParseIdListQuotesSpecAsTyped(t *testing.T) {
	_, err := parseSelection("1 abc")
	want := `invalid id "abc" in spec "1 abc"`
	if err == nil || err.Error() != want {
		t.Errorf("parseSelection error = %v, want %q", err, want)
	}
}
