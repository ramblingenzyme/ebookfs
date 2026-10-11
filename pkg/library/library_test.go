package library

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

// Every case starts from the same Meta, so what a nil edit leaves alone is
// checked alongside what a set one changes.
//
// Tags is the one field a value receiver does not copy. Element assignment
// detects the sharing where append would not.
func TestApplyMeta(t *testing.T) {
	start := book.Meta{ID: 1, Status: "unread", Rating: 2.5, Tags: []string{"keep"}}

	tests := []struct {
		name   string
		edits  Edits
		status string
		rating float64
		tags   []string
	}{
		{"no edits", Edits{}, "unread", 2.5, []string{"keep"}},
		{"status only", Edits{Status: new("read")}, "read", 2.5, []string{"keep"}},
		{"rating only", Edits{Rating: new(4.5)}, "unread", 4.5, []string{"keep"}},
		{"tags only", Edits{Tags: new([]string{"new", "tags"})}, "unread", 2.5, []string{"new", "tags"}},
		// Clearing tags is a set edit to an empty slice, not an absent one.
		{"tags cleared", Edits{Tags: new([]string{})}, "unread", 2.5, []string{}},
		{
			"all fields",
			Edits{Status: new("read"), Rating: new(5.0), Tags: new([]string{"all"})},
			"read", 5.0, []string{"all"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var editTags []string
			if tc.edits.Tags != nil {
				editTags = slices.Clone(*tc.edits.Tags)
			}
			before := time.Now()
			updated := applyMeta(start, tc.edits)

			if updated.Status != tc.status {
				t.Errorf("Status = %q, want %q", updated.Status, tc.status)
			}
			if updated.Rating != tc.rating {
				t.Errorf("Rating = %g, want %g", updated.Rating, tc.rating)
			}
			if !slices.Equal(updated.Tags, tc.tags) || (updated.Tags == nil) != (tc.tags == nil) {
				t.Errorf("Tags = %#v, want %#v", updated.Tags, tc.tags)
			}
			if updated.ID != start.ID {
				t.Errorf("ID = %d, want %d — applyMeta must not touch identity", updated.ID, start.ID)
			}
			// Bumped even when no field changed, so the sidecar write does not
			// look older than the file.
			if updated.DateModified.Before(before) {
				t.Errorf("DateModified = %v, want it stamped at or after %v", updated.DateModified, before)
			}

			if len(updated.Tags) > 0 {
				updated.Tags[0] = "changed"
			}
			if !slices.Equal(start.Tags, []string{"keep"}) {
				t.Fatalf("start Tags = %v, want [keep] — the result shares the argument's backing array", start.Tags)
			}
			if tc.edits.Tags != nil && !slices.Equal(*tc.edits.Tags, editTags) {
				t.Errorf("edit Tags = %v, want %v — the result shares the edit's backing array", *tc.edits.Tags, editTags)
			}
		})
	}

	// Cloning must not turn an absent tag list into an empty one: the sidecar
	// writer distinguishes them.
	if got := applyMeta(book.Meta{ID: 1}, Edits{}).Tags; got != nil {
		t.Errorf("Tags = %v, want nil", got)
	}
}

// Every Edits field must reach the epub rewrite, the cover, or the sidecar, and
// nothing in the types enforces it. This catches a field routed nowhere, or
// into apply but not HasBibEdits. It misses one in HasBibEdits but not apply;
// library_ext_test.go checks effects.
func TestEveryEditFieldIsRouted(t *testing.T) {
	// applyMeta's three, which have no predicate of their own to ask.
	meta := map[string]bool{"Status": true, "Rating": true, "Tags": true}

	v := reflect.ValueOf(&Edits{}).Elem()
	for i := range v.NumField() {
		name := v.Type().Field(i).Name
		t.Run(name, func(t *testing.T) {
			var e Edits
			f := reflect.ValueOf(&e).Elem().Field(i)
			f.Set(reflect.New(f.Type().Elem()))

			if e.HasBibEdits() || e.HasCoverEdit() || meta[name] {
				return
			}
			t.Errorf("Edits.%s reaches no destination: add it to HasBibEdits and "+
				"internal/epub's apply, to HasCoverEdit, or to applyMeta and this "+
				"test's meta list", name)
		})
	}
}
