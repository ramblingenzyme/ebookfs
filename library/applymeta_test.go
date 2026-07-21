package library

import (
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

func TestApplyMetaOnlyStatus(t *testing.T) {
	meta := model.Meta{ID: 1, Status: "unread", Rating: 3}

	status := "read"
	before := time.Now()
	updated := applyMeta(meta, model.Edits{Status: &status})

	if updated.Status != "read" {
		t.Errorf("Status = %q, want %q", updated.Status, "read")
	}
	if meta.Status == "read" {
		t.Error("Edit should not apply to the original object")
	}

	if updated.Rating != 3 {
		t.Errorf("Rating should be unchanged, got %g", updated.Rating)
	}
	if updated.DateModified.Before(before) {
		t.Errorf("DateModified should be updated")
	}
}

func TestApplyMetaOnlyRating(t *testing.T) {
	meta := model.Meta{ID: 1, Status: "unread", Rating: 3}

	rating := 4.5
	updated := applyMeta(meta, model.Edits{Rating: &rating})

	if updated.Rating != 4.5 {
		t.Errorf("Rating = %g, want %g", updated.Rating, 4.5)
	}
	if updated.Status != "unread" {
		t.Errorf("Status should be unchanged, got %q", updated.Status)
	}
}

func TestApplyMetaOnlyTags(t *testing.T) {
	meta := model.Meta{ID: 1, Tags: []string{"old"}}

	tags := []string{"new", "tags"}
	updated := applyMeta(meta, model.Edits{Tags: &tags})

	if len(updated.Tags) != 2 || updated.Tags[0] != "new" {
		t.Errorf("Tags = %v, want %v", updated.Tags, tags)
	}
}

func TestApplyMetaAllFields(t *testing.T) {
	meta := model.Meta{ID: 1, Status: "unread", Rating: 1}

	status := "read"
	rating := 5.0
	tags := []string{"all"}
	updated := applyMeta(meta, model.Edits{Status: &status, Rating: &rating, Tags: &tags})

	if updated.Status != "read" {
		t.Errorf("Status = %q", updated.Status)
	}
	if updated.Rating != 5.0 {
		t.Errorf("Rating = %g", updated.Rating)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "all" {
		t.Errorf("Tags = %v", updated.Tags)
	}
}

func TestApplyMetaNoEdits(t *testing.T) {
	meta := model.Meta{ID: 1, Status: "unread", Rating: 2.5, Tags: []string{"keep"}}

	updated := applyMeta(meta, model.Edits{})

	if updated.Status != "unread" {
		t.Errorf("Status changed to %q", updated.Status)
	}
	if updated.Rating != 2.5 {
		t.Errorf("Rating changed to %g", updated.Rating)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "keep" {
		t.Errorf("Tags changed to %v", updated.Tags)
	}
	// DateModified should still be bumped even with no field edits.
	if updated.DateModified.IsZero() {
		t.Error("DateModified should be set")
	}
}
