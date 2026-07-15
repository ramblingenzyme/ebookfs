package book

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

func TestAuthorsFieldGet(t *testing.T) {
	tests := []struct {
		name string
		book *model.Book
		want string
	}{
		{
			"no authors",
			model.NewBook(model.Bib{Title: "T"}, model.Meta{ID: 1}, model.Location{}),
			"",
		},
		{
			"no sort name",
			model.NewBook(model.Bib{Title: "T", Authors: []model.Author{{Name: "Alice"}}}, model.Meta{ID: 1}, model.Location{}),
			"Alice",
		},
		{
			"with sort name",
			model.NewBook(
				model.Bib{Title: "T", Authors: []model.Author{{Name: "Alice", SortName: "Smith, Alice"}}},
				model.Meta{ID: 1},
				model.Location{},
			),
			"Alice | Smith, Alice",
		},
		{
			"multi-author mixed",
			model.NewBook(
				model.Bib{Title: "T", Authors: []model.Author{
					{Name: "Alice", SortName: "Smith, Alice"},
					{Name: "Bob"},
				}},
				model.Meta{ID: 1},
				model.Location{},
			),
			"Alice | Smith, Alice\nBob",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fields["authors"].get(tt.book)
			if got != tt.want {
				t.Errorf("get = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAuthorsFieldEdits(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []model.Author
		wantErr bool
	}{
		{
			"name only",
			"Bob",
			[]model.Author{{Name: "Bob"}},
			false,
		},
		{
			"name and sort name",
			"Bob | Jones, Bob",
			[]model.Author{{Name: "Bob", SortName: "Jones, Bob"}},
			false,
		},
		{
			"bare pipe",
			"Bob|Jones, Bob",
			[]model.Author{{Name: "Bob", SortName: "Jones, Bob"}},
			false,
		},
		{
			"multi-line",
			"Alice\nBob",
			[]model.Author{{Name: "Alice"}, {Name: "Bob"}},
			false,
		},
		{
			"multi-line with sort names",
			"Alice | Smith, Alice\nBob | Jones, Bob",
			[]model.Author{{Name: "Alice", SortName: "Smith, Alice"}, {Name: "Bob", SortName: "Jones, Bob"}},
			false,
		},
		{
			"empty lines skipped",
			"\n\nAlice\n\nBob\n",
			[]model.Author{{Name: "Alice"}, {Name: "Bob"}},
			false,
		},
		{
			"empty input rejected",
			"",
			nil,
			true,
		},
		{
			"whitespace only rejected",
			"  \n  \n",
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits, err := fields["authors"].edits(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("edits error = %v, wantErr = %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if tt.want == nil {
				if edits.Authors != nil {
					t.Errorf("Authors = %v, want nil", *edits.Authors)
				}
				return
			}
			if edits.Authors == nil {
				t.Fatal("Authors is nil")
			}
			got := *edits.Authors
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].Name != tt.want[i].Name {
					t.Errorf("[%d].Name = %q, want %q", i, got[i].Name, tt.want[i].Name)
				}
				if got[i].SortName != tt.want[i].SortName {
					t.Errorf("[%d].SortName = %q, want %q", i, got[i].SortName, tt.want[i].SortName)
				}
			}
		})
	}
}
