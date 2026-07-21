package library

import (
	"slices"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// TestApplyMeta covers the field-by-field application. Every case starts from
// the same Meta so what a nil edit leaves alone is asserted alongside what a set
// one changes — applyMeta's whole job is the boundary between those two.
//
// Independence of the result is TestApplyMetaClonesTags' job; the value receiver
// makes it uninteresting for every field except Tags.
func TestApplyMeta(t *testing.T) {
	start := model.Meta{ID: 1, Status: "unread", Rating: 2.5, Tags: []string{"keep"}}

	tests := []struct {
		name   string
		edits  model.Edits
		status string
		rating float64
		tags   []string
	}{
		{"no edits", model.Edits{}, "unread", 2.5, []string{"keep"}},
		{"status only", model.Edits{Status: ptr("read")}, "read", 2.5, []string{"keep"}},
		{"rating only", model.Edits{Rating: ptr(4.5)}, "unread", 4.5, []string{"keep"}},
		{"tags only", model.Edits{Tags: ptr([]string{"new", "tags"})}, "unread", 2.5, []string{"new", "tags"}},
		// Clearing tags is a set edit to an empty slice, not an absent one.
		{"tags cleared", model.Edits{Tags: ptr([]string{})}, "unread", 2.5, nil},
		{
			"all fields",
			model.Edits{Status: ptr("read"), Rating: ptr(5.0), Tags: ptr([]string{"all"})},
			"read", 5.0, []string{"all"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := time.Now()
			updated := applyMeta(start, tc.edits)

			if updated.Status != tc.status {
				t.Errorf("Status = %q, want %q", updated.Status, tc.status)
			}
			if updated.Rating != tc.rating {
				t.Errorf("Rating = %g, want %g", updated.Rating, tc.rating)
			}
			if !slices.Equal(updated.Tags, tc.tags) {
				t.Errorf("Tags = %v, want %v", updated.Tags, tc.tags)
			}
			if updated.ID != start.ID {
				t.Errorf("ID = %d, want %d — applyMeta must not touch identity", updated.ID, start.ID)
			}
			// Bumped even when no field changed: the edit still happened, and
			// the sidecar write that follows must not look older than the file.
			if updated.DateModified.Before(before) {
				t.Errorf("DateModified = %v, want it stamped at or after %v", updated.DateModified, before)
			}
		})
	}
}

// TestApplyMetaClonesTags is the one field a value receiver does not make
// independent. Tags comes either from the Meta passed in or from the Edits, both
// of which the caller still holds while the result travels on to the sidecar
// write and the index — so writing through one must not be visible through the
// other. Element assignment is what detects the sharing: appending would not,
// since a len-1 slice hides a write past its own end.
func TestApplyMetaClonesTags(t *testing.T) {
	t.Run("from the meta", func(t *testing.T) {
		meta := model.Meta{ID: 1, Tags: []string{"keep"}}

		updated := applyMeta(meta, model.Edits{})
		updated.Tags[0] = "changed"

		if meta.Tags[0] != "keep" {
			t.Errorf("original Tags = %v, want [keep] — the result shares the argument's backing array", meta.Tags)
		}
	})

	t.Run("from the edits", func(t *testing.T) {
		tags := []string{"new"}

		updated := applyMeta(model.Meta{ID: 1}, model.Edits{Tags: &tags})
		updated.Tags[0] = "changed"

		if tags[0] != "new" {
			t.Errorf("edit Tags = %v, want [new] — the result shares the edit's backing array", tags)
		}
	})

	t.Run("nil stays nil", func(t *testing.T) {
		// Cloning must not turn an absent tag list into an empty one: the
		// sidecar writer distinguishes them.
		if got := applyMeta(model.Meta{ID: 1}, model.Edits{}).Tags; got != nil {
			t.Errorf("Tags = %v, want nil", got)
		}
	})
}
