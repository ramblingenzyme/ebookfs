package model

import (
	"fmt"
	"math"
	"strings"

	"golang.org/x/text/language"
)

// Edits is a partial update to a Book's fields. A nil pointer leaves the field
// untouched; a non-nil pointer (including one to a zero value) applies the
// change. This lets a caller change exactly one field — e.g. just the title —
// without having to supply the rest.
//
// A non-nil Series pointing at "" removes the series. SeriesIndex applied
// without Series ("index-only" edit) updates the position of the book's
// current series, resolved against the live snapshot under the per-book lock.
//
// SortTitle follows the same nil/empty rules. As a special case, changing Title
// without supplying a SortTitle clears any existing sort title — it was derived
// from the old title, so leaving it would make the sort title disagree with the
// title.
type Edits struct {
	// Bib fields (written to the epub OPF).
	Title       *string
	SortTitle   *string
	Description *string
	Language    *string
	Authors     *[]Author
	Series      *string
	SeriesIndex *float64

	// Cover replaces the cover image in the epub. It is applied before any bib
	// edits so that a combined Cover + OPF edit produces a single final epub
	// rewrite. Cover replaces the old Library.WriteCover method: all mutations
	// now flow through Edit, keeping the registry snapshot self-consistent.

	Cover *[]byte

	// Meta fields (written to the meta.toml sidecar).
	Status *string
	Rating *float64
	Tags   *[]string
}

// Normalized returns a copy of e with the persisted-precision rules applied:
// ratings are stored to 2 decimal places and series indices to 1. Rounding is
// a storage policy enforced by Library.Edit for every caller, not a parsing
// concern re-implemented by each frontend. NaN/Inf survive rounding and are
// rejected by Validate.
func (e Edits) Normalized() Edits {
	if e.Rating != nil {
		r := math.Round(*e.Rating*100) / 100
		e.Rating = &r
	}
	if e.SeriesIndex != nil {
		i := math.Round(*e.SeriesIndex*10) / 10
		e.SeriesIndex = &i
	}
	return e
}

// HasBibEdits reports whether any OPF-level field is non-nil.
func (e Edits) HasBibEdits() bool {
	return e.Title != nil || e.SortTitle != nil || e.Description != nil ||
		e.Language != nil || e.Authors != nil || e.Series != nil || e.SeriesIndex != nil
}

// HasCoverEdit reports whether a cover image replacement is requested.
func (e Edits) HasCoverEdit() bool { return e.Cover != nil }

// FieldError pairs a field name with a human-readable validation error message,
// so frontends can display feedback next to the relevant field.
type FieldError struct {
	Field   string
	Message string
}

func (fe FieldError) Error() string { return fe.Field + ": " + fe.Message }

// ValidationError collects per-field validation errors from Validate(). It
// satisfies the error interface for simple "if err != nil" checks; callers can
// use errors.As to recover the structured field map.
type ValidationError []FieldError

func (ve ValidationError) Error() string {
	switch len(ve) {
	case 0:
		return ""
	case 1:
		return ve[0].Error()
	}
	// Multi-field: "field1: msg; field2: msg"
	var s strings.Builder
	for i, fe := range ve {
		if i > 0 {
			s.WriteString("; ")
		}
		s.WriteString(fe.Field)
		s.WriteString(": ")
		s.WriteString(fe.Message)
	}
	return s.String()
}

// fieldValidator pairs a field name with its validation function.
type fieldValidator struct {
	field    string
	validate func() string // returns error message or ""
}

// Validate validates e against b's current state and returns per-field errors.
// A nil return means all fields are valid.
func (e Edits) Validate(b *Book) *ValidationError {
	validators := []fieldValidator{
		{"status", e.validateStatus},
		{"rating", e.validateRating},
		{"title", e.validateTitle},
		{"authors", e.validateAuthors},
		{"tags", e.validateTags},
		{"language", e.validateLanguage},
		{"cover", func() string { return e.validateCover(b) }},
		{"series_index", func() string { return e.validateSeriesIndex(b) }},
	}

	var ve ValidationError
	for _, v := range validators {
		if msg := v.validate(); msg != "" {
			ve = append(ve, FieldError{Field: v.field, Message: msg})
		}
	}

	if len(ve) == 0 {
		return nil
	}
	return &ve
}

func (e Edits) validateStatus() string {
	if e.Status != nil && !IsValidStatus(*e.Status) {
		return fmt.Sprintf("invalid status %q: must be %s", *e.Status, StatusList())
	}
	return ""
}

func (e Edits) validateRating() string {
	if e.Rating == nil {
		return ""
	}
	// NaN compares false against both bounds, and once persisted it bricks the
	// index (SQLite binds NaN as NULL, violating the NOT NULL rating column),
	// so it must be rejected here.
	if math.IsNaN(*e.Rating) || *e.Rating < 0 || *e.Rating > 5 {
		return fmt.Sprintf("invalid rating %g: must be 0-5", *e.Rating)
	}
	return ""
}

func (e Edits) validateTitle() string {
	if e.Title != nil && strings.TrimSpace(*e.Title) == "" {
		return "title must not be empty"
	}
	return ""
}

func (e Edits) validateAuthors() string {
	if e.Authors == nil {
		return ""
	}
	if len(*e.Authors) == 0 {
		return "at least one author is required"
	}
	for i, a := range *e.Authors {
		if strings.TrimSpace(a.Name) == "" {
			return fmt.Sprintf("author %d has an empty name", i+1)
		}
	}
	return ""
}

func (e Edits) validateTags() string {
	if e.Tags == nil {
		return ""
	}
	for i, t := range *e.Tags {
		if strings.TrimSpace(t) == "" {
			return fmt.Sprintf("tag %d is empty", i+1)
		}
	}
	return ""
}

func (e Edits) validateLanguage() string {
	if e.Language == nil {
		return ""
	}
	if v := strings.TrimSpace(*e.Language); v != "" {
		if _, err := language.Parse(v); err != nil {
			return fmt.Sprintf("language %q is not a recognised BCP 47 / ISO 639 code", *e.Language)
		}
	}
	return ""
}

func (e Edits) validateCover(b *Book) string {
	if e.Cover == nil {
		return ""
	}
	if len(*e.Cover) == 0 {
		return "cover image must not be empty"
	}
	if b.CoverPath == "" {
		return "book has no cover to replace"
	}
	return ""
}

func (e Edits) validateSeriesIndex(b *Book) string {
	if e.SeriesIndex == nil {
		return ""
	}
	if math.IsNaN(*e.SeriesIndex) || math.IsInf(*e.SeriesIndex, 0) {
		return fmt.Sprintf("invalid series index %g: must be a finite number", *e.SeriesIndex)
	}
	if e.Series == nil && b.Series == nil {
		return "book has no series to set an index on"
	}
	return ""
}
