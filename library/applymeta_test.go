package library

import (
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

func TestApplyMetaOnlyStatus(t *testing.T) {
	book := model.NewBook(
		model.Bib{Title: "Test", Authors: []model.Author{{Name: "Alice"}}},
		model.Meta{ID: 1, Status: "unread", Rating: 3},
		model.Location{},
	)

	status := "read"
	before := time.Now()
	updated := applyMeta(book, model.Edits{Status: &status})

	if updated.Meta.Status != "read" {
		t.Errorf("Status = %q, want %q", updated.Meta.Status, "read")
	}
	if updated.Meta.Rating != 3 {
		t.Errorf("Rating should be unchanged, got %g", updated.Meta.Rating)
	}
	if updated.Meta.DateModified.Before(before) {
		t.Errorf("DateModified should be updated")
	}
}

func TestApplyMetaOnlyRating(t *testing.T) {
	book := model.NewBook(
		model.Bib{Title: "Test", Authors: []model.Author{{Name: "Alice"}}},
		model.Meta{ID: 1, Status: "unread", Rating: 3},
		model.Location{},
	)

	rating := 4.5
	updated := applyMeta(book, model.Edits{Rating: &rating})

	if updated.Meta.Rating != 4.5 {
		t.Errorf("Rating = %g, want %g", updated.Meta.Rating, 4.5)
	}
	if updated.Meta.Status != "unread" {
		t.Errorf("Status should be unchanged, got %q", updated.Meta.Status)
	}
}

func TestApplyMetaOnlyTags(t *testing.T) {
	book := model.NewBook(
		model.Bib{Title: "Test", Authors: []model.Author{{Name: "Alice"}}},
		model.Meta{ID: 1, Tags: []string{"old"}},
		model.Location{},
	)

	tags := []string{"new", "tags"}
	updated := applyMeta(book, model.Edits{Tags: &tags})

	if len(updated.Meta.Tags) != 2 || updated.Meta.Tags[0] != "new" {
		t.Errorf("Tags = %v, want %v", updated.Meta.Tags, tags)
	}
}

func TestApplyMetaAllFields(t *testing.T) {
	book := model.NewBook(
		model.Bib{Title: "Test", Authors: []model.Author{{Name: "Alice"}}},
		model.Meta{ID: 1, Status: "unread", Rating: 1},
		model.Location{},
	)

	status := "read"
	rating := 5.0
	tags := []string{"all"}
	updated := applyMeta(book, model.Edits{Status: &status, Rating: &rating, Tags: &tags})

	if updated.Meta.Status != "read" {
		t.Errorf("Status = %q", updated.Meta.Status)
	}
	if updated.Meta.Rating != 5.0 {
		t.Errorf("Rating = %g", updated.Meta.Rating)
	}
	if len(updated.Meta.Tags) != 1 || updated.Meta.Tags[0] != "all" {
		t.Errorf("Tags = %v", updated.Meta.Tags)
	}
}

func TestFormatAuthors(t *testing.T) {
	tests := []struct {
		name    string
		authors []model.Author
		want    string
	}{
		{"single", []model.Author{{Name: "Alice"}}, "Alice"},
		{"multiple", []model.Author{{Name: "Alice"}, {Name: "Bob"}}, "Alice, Bob"},
		{"empty names", []model.Author{{Name: ""}, {Name: ""}}, model.UnknownAuthor},
		{"mixed empty and valid", []model.Author{{Name: ""}, {Name: "Alice"}}, "Alice"},
		{"nil", nil, model.UnknownAuthor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAuthors(tt.authors)
			if got != tt.want {
				t.Errorf("formatAuthors(%v) = %q, want %q", tt.authors, got, tt.want)
			}
		})
	}
}

func TestApplyMetaNoEdits(t *testing.T) {
	book := model.NewBook(
		model.Bib{Title: "Test", Authors: []model.Author{{Name: "Alice"}}},
		model.Meta{ID: 1, Status: "unread", Rating: 2.5, Tags: []string{"keep"}},
		model.Location{},
	)

	updated := applyMeta(book, model.Edits{})

	if updated.Meta.Status != "unread" {
		t.Errorf("Status changed to %q", updated.Meta.Status)
	}
	if updated.Meta.Rating != 2.5 {
		t.Errorf("Rating changed to %g", updated.Meta.Rating)
	}
	if len(updated.Meta.Tags) != 1 || updated.Meta.Tags[0] != "keep" {
		t.Errorf("Tags changed to %v", updated.Meta.Tags)
	}
	// DateModified should still be bumped even with no field edits.
	if updated.Meta.DateModified.IsZero() {
		t.Error("DateModified should be set")
	}
}
