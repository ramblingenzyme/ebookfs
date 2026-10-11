package book

import (
	"reflect"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// Most fields pass their text through, so a row pins which Edits field it lands
// in. List fields read back in the shape they were written, without the blank
// lines.
func TestFieldEdits(t *testing.T) {
	for _, tc := range []struct {
		field   string
		input   string
		want    library.Edits
		wantErr bool
		reads   string // the file's text after the edit; checked for list fields
	}{
		{field: "status", input: "reading", want: library.Edits{Status: new("reading")}},
		{field: "rating", input: "4.5", want: library.Edits{Rating: new(4.5)}},
		{field: "rating", input: "seven", wantErr: true},
		{field: "title", input: "Dune", want: library.Edits{Title: new("Dune")}},
		{field: "language", input: "en", want: library.Edits{Language: new("en")}},
		{field: "description", input: "A desert.", want: library.Edits{Description: new("A desert.")}},
		{field: "publisher", input: "Chilton", want: library.Edits{Publisher: new("Chilton")}},
		{field: "rights", input: "All rights reserved", want: library.Edits{Rights: new("All rights reserved")}},
		{field: "series", input: "Dune", want: library.Edits{Series: new("Dune")}},
		{field: "series_index", input: "2.1", want: library.Edits{SeriesIndex: new("2.1")}},
		{field: "tags", input: "sf\n\nclassic\n", want: library.Edits{Tags: &[]string{"sf", "classic"}},
			reads: "sf\nclassic"},
		{field: "subjects", input: "\nFiction\nDeserts", want: library.Edits{Subjects: &[]string{"Fiction", "Deserts"}},
			reads: "Fiction\nDeserts"},
		{field: "contributors", input: "Ann | ill\n\n  Bob  \n | edt", want: library.Edits{
			Contributors: &[]library.Contributor{{Name: "Ann", Role: "ill"}, {Name: "Bob"}},
		}, reads: "Ann | ill\nBob"},
		{field: "authors", input: "Alice\nBob", want: library.Edits{
			Authors: &[]library.Author{{Name: "Alice"}, {Name: "Bob"}},
		}, reads: "Alice\nBob"},
		{field: "authors", input: "Alice | Smith, Alice\nBob | Jones, Bob", want: library.Edits{
			Authors: &[]library.Author{{Name: "Alice", SortName: "Smith, Alice"}, {Name: "Bob", SortName: "Jones, Bob"}},
		}, reads: "Alice | Smith, Alice\nBob | Jones, Bob"},
		{field: "authors", input: "\n\nAlice\n\nBob\n", want: library.Edits{
			Authors: &[]library.Author{{Name: "Alice"}, {Name: "Bob"}},
		}, reads: "Alice\nBob"},
		{field: "authors", input: "", wantErr: true},
		{field: "authors", input: "  \n  \n", wantErr: true},
	} {
		t.Run(tc.field+" "+tc.input, func(t *testing.T) {
			got, err := fields[tc.field].edits(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("edits error = %v, wantErr = %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("edits = %+v, want %+v", got, tc.want)
			}
			if tc.reads == "" {
				return
			}
			b := util.MakeMutableBook(1, "T", "Author")
			b.Meta.Tags = deref(got.Tags)
			b.Subjects = deref(got.Subjects)
			b.Contributors = deref(got.Contributors)
			b.Authors = deref(got.Authors)
			if text := fields[tc.field].get(util.WrapBook(b)); text != tc.reads {
				t.Errorf("get after the edit = %q, want %q", text, tc.reads)
			}
		})
	}
}

func deref[T any](p *[]T) []T {
	if p == nil {
		return nil
	}
	return *p
}
