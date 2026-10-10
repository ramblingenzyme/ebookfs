package book

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"golang.org/x/text/language"

	"github.com/ramblingenzyme/ebookfs/internal/util/naming"
)

// Edits is a partial update to a Book's fields. A nil pointer leaves the field
// untouched; a non-nil pointer (including one to a zero value) applies the
// change. This lets a caller change exactly one field, just the title say,
// without having to supply the rest.
//
// A non-nil Series pointing at "" removes the series. SeriesIndex applied
// without Series ("index-only" edit) updates the position of the book's
// current series, resolved against the live snapshot under the per-book lock.
//
// SortTitle follows the same nil/empty rules. Changing Title without a SortTitle
// clears the sort title; package library/internal/epub applies that rule.
type Edits struct {
	// Bib fields (written to the epub OPF).
	Title        *string
	SortTitle    *string
	Description  *string
	Language     *string
	Publisher    *string
	Rights       *string
	Authors      *[]Author
	Series       *string
	SeriesIndex  *string
	Subjects     *[]string
	Contributors *[]Contributor

	Cover *[]byte

	// Meta fields (written to the meta.toml sidecar).
	Status *string
	Rating *float64
	Tags   *[]string
}

// Normalized returns a copy of e with the persisted-precision rules applied:
// ratings are stored to 2 decimal places. Rounding is a storage policy enforced
// by Library.Edit for every caller, not a parsing concern re-implemented by each
// frontend. NaN/Inf survive rounding and are rejected by Validate.
//
// The series index is not rounded: it is the string the epub carries, and
// D.3.7's multi-level positions have no precision to round to.
func (e Edits) Normalized() Edits {
	if e.Rating != nil {
		r := math.Round(*e.Rating*100) / 100
		e.Rating = &r
	}
	return e
}

func (e Edits) HasBibEdits() bool {
	return e.Title != nil || e.SortTitle != nil || e.Description != nil ||
		e.Language != nil || e.Publisher != nil || e.Rights != nil ||
		e.Authors != nil || e.Series != nil || e.SeriesIndex != nil ||
		e.Subjects != nil || e.Contributors != nil
}

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

func Validate(e Edits, b *Book) *ValidationError {
	validators := []fieldValidator{
		{"status", e.validateStatus},
		{"rating", e.validateRating},
		{"title", e.validateTitle},
		{"authors", e.validateAuthors},
		{"tags", e.validateTags},
		{"subjects", e.validateSubjects},
		{"contributors", e.validateContributors},
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
	name := func(a Author) string { return strings.TrimSpace(a.Name) }
	if i := firstRepeat(*e.Authors, name); i >= 0 {
		return fmt.Sprintf("author %d duplicates %q", i+1, name((*e.Authors)[i]))
	}
	return ""
}

// validateTags rejects a tag that names no directory. A frontend groups books
// by tag, and naming.PathSafe gives every value that trims away, "." and ".."
// among them, one placeholder, so accepting them files unrelated tags together.
func (e Edits) validateTags() string {
	if e.Tags == nil {
		return ""
	}
	for i, t := range *e.Tags {
		if strings.TrimSpace(t) == "" {
			return fmt.Sprintf("tag %d is empty", i+1)
		}
		if naming.NamesNothing(t) {
			return fmt.Sprintf("tag %d is %q, which cannot name a directory", i+1, t)
		}
	}
	if i := firstRepeat(*e.Tags, strings.TrimSpace); i >= 0 {
		return fmt.Sprintf("tag %d duplicates %q", i+1, strings.TrimSpace((*e.Tags)[i]))
	}
	return ""
}

func (e Edits) validateSubjects() string {
	if e.Subjects == nil {
		return ""
	}
	if i := firstRepeat(*e.Subjects, strings.TrimSpace); i >= 0 {
		return fmt.Sprintf("subject %d duplicates %q", i+1, strings.TrimSpace((*e.Subjects)[i]))
	}
	return ""
}

// validateContributors rejects one person credited twice in the same role. A
// person may hold several roles, so the key is the pair.
func (e Edits) validateContributors() string {
	if e.Contributors == nil {
		return ""
	}
	cs := *e.Contributors
	i := firstRepeat(cs, func(c Contributor) Contributor { return c })
	switch {
	case i < 0:
		return ""
	case cs[i].Role == "":
		return fmt.Sprintf("contributor %d duplicates %q", i+1, cs[i].Name)
	default:
		return fmt.Sprintf("contributor %d duplicates %q as %s", i+1, cs[i].Name, cs[i].Role)
	}
}

// firstRepeat returns the index of the first element whose key an earlier one
// shares, or -1.
func firstRepeat[T any, K comparable](xs []T, key func(T) K) int {
	seen := make(map[K]bool, len(xs))
	for i, x := range xs {
		k := key(x)
		if seen[k] {
			return i
		}
		seen[k] = true
	}
	return -1
}

// validateLanguage rejects an empty value. EPUB 3.3 §5.5.3.1 and
// OPF 2.0 §2.2.12 both require a dc:language, so empty cannot mean unset.
func (e Edits) validateLanguage() string {
	if e.Language == nil {
		return ""
	}
	v := strings.TrimSpace(*e.Language)
	if v == "" {
		return "language must not be empty"
	}
	if _, err := language.Parse(v); err != nil {
		return fmt.Sprintf("language %q is not a recognised BCP 47 / ISO 639 code", *e.Language)
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

// seriesIndexPattern is EPUB 3.3 Appendix D.3.7's allowed value: "A single
// xsd:unsignedInt or series of decimal-separated numbers (e.g., 1 or 2.2.1)."
var seriesIndexPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)

// ValidSeriesIndex reports whether s is a well-formed series position. It is
// exported so the epub reader can apply the same grammar it validates edits
// against, rather than keeping a second opinion about what a position is.
func ValidSeriesIndex(s string) bool { return seriesIndexPattern.MatchString(s) }

func (e Edits) validateSeriesIndex(b *Book) string {
	if e.SeriesIndex == nil {
		return ""
	}
	if !ValidSeriesIndex(*e.SeriesIndex) {
		return fmt.Sprintf("invalid series index %q: must be a number, or decimal-separated numbers such as 2.2.1", *e.SeriesIndex)
	}
	switch {
	case e.Series != nil && *e.Series == "":
		return "series index set while clearing the series"
	case e.Series == nil && b.Series == nil:
		return "book has no series to set an index on"
	}
	return ""
}
