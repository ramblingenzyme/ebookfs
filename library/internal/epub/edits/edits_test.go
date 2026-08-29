package edits

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// testBook creates a Book for validation tests with the given cover path and series.
func testBook(coverPath string, series *model.SeriesRef) *book.Book {
	return &book.Book{
		Bib: book.Bib{
			CoverPath: coverPath,
			Series:    series,
		},
	}
}

// assertSingleFieldError asserts err is a *ValidationError carrying exactly one
// entry, on the named field.
func assertSingleFieldError(t *testing.T, err error, field string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error is not *ValidationError: %T", err)
	}
	if len(*ve) != 1 || (*ve)[0].Field != field {
		t.Errorf("expected a single %q field error, got %v", field, *ve)
	}
}

// assertHasFieldError asserts err is a *ValidationError with at least one entry
// on the named field whose message contains msgSubstr (empty matches any).
func assertHasFieldError(t *testing.T, err error, field, msgSubstr string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error is not *ValidationError: %T", err)
	}
	for _, fe := range *ve {
		if fe.Field == field && strings.Contains(fe.Message, msgSubstr) {
			return
		}
	}
	t.Errorf("expected an error on field %q containing %q, got %v", field, msgSubstr, *ve)
}

func TestValidateStatus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  string
		wantErr bool
	}{
		{"unread", "unread", false},
		{"reading", "reading", false},
		{"read", "read", false},
		{"abandoned", "abandoned", false},
		{"invalid", "finished", true},
		{"empty", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := Edits{Status: new(tc.status)}
			err := Validate(e, testBook("", nil))
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				assertSingleFieldError(t, err, "status")
			}
		})
	}
}

func TestValidateRating(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rating  float64
		wantErr bool
	}{
		{"zero", 0, false},
		{"five", 5, false},
		{"three", 3, false},
		{"decimal", 4.75, false},
		{"negative", -1, true},
		{"too high", 6, true},
		{"NaN", math.NaN(), true},
		{"positive infinity", math.Inf(1), true},
		{"negative infinity", math.Inf(-1), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := Edits{Rating: new(tc.rating)}
			err := Validate(e, testBook("", nil))
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				assertSingleFieldError(t, err, "rating")
			}
		})
	}
}

func TestValidateTitle(t *testing.T) {
	for _, tc := range []struct {
		name    string
		title   string
		wantErr bool
	}{
		{"valid", "Some Title", false},
		{"empty", "", true},
		{"whitespace", "   ", true},
		{"tabs", "\t\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := Edits{Title: new(tc.title)}
			err := Validate(e, testBook("", nil))
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				assertSingleFieldError(t, err, "title")
			}
		})
	}
}

func TestValidateAuthors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		authors *[]model.Author
		wantErr bool
		errMsg  string
	}{
		{"nil untouched", nil, false, ""},
		{"valid single", &[]model.Author{{Name: "Alice"}}, false, ""},
		{"valid multiple", &[]model.Author{{Name: "Alice"}, {Name: "Bob"}}, false, ""},
		{"empty slice", &[]model.Author{}, true, "at least one author"},
		{"empty name", &[]model.Author{{Name: ""}}, true, "author 1 has an empty name"},
		{"second empty name", &[]model.Author{{Name: "Alice"}, {Name: ""}}, true, "author 2 has an empty name"},
		{"whitespace name", &[]model.Author{{Name: "   "}}, true, "author 1 has an empty name"},
		{"duplicate name", &[]model.Author{{Name: "Alice"}, {Name: "Alice"}}, true, `author 2 duplicates "Alice"`},
		{"duplicate after trimming", &[]model.Author{{Name: "Alice"}, {Name: " Alice "}}, true, `author 2 duplicates "Alice"`},
		{"duplicate sort names are fine", &[]model.Author{{Name: "Alice", SortName: "X"}, {Name: "Bob", SortName: "X"}}, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := Edits{Authors: tc.authors}
			err := Validate(e, testBook("", nil))
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && tc.errMsg != "" {
				assertHasFieldError(t, err, "authors", tc.errMsg)
			}
		})
	}
}

func TestValidateLanguage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		lang    string
		wantErr bool
	}{
		{"english", "en", false},
		{"french", "fr", false},
		{"bcp47", "en-US", false},
		{"empty unset", "", false},
		{"whitespace unset", "   ", false},
		{"invalid", "123", true},
		{"gibberish", "xyz123abc", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := Edits{Language: new(tc.lang)}
			err := Validate(e, testBook("", nil))
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				assertSingleFieldError(t, err, "language")
			}
		})
	}
}

func TestValidateTags(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tags    *[]string
		wantErr bool
	}{
		{"nil untouched", nil, false},
		{"valid", &[]string{"fiction", "sci-fi"}, false},
		{"single valid", &[]string{"fiction"}, false},
		{"empty tag", &[]string{""}, true},
		{"whitespace tag", &[]string{"  "}, true},
		{"mixed valid and empty", &[]string{"good", "", "bad"}, true},
		{"empty first", &[]string{"", "good"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := Edits{Tags: tc.tags}
			err := Validate(e, testBook("", nil))
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				assertHasFieldError(t, err, "tags", "")
			}
		})
	}
}

func TestValidateSeriesIndex(t *testing.T) {
	for _, tc := range []struct {
		name       string
		bookSeries *model.SeriesRef
		edits      Edits
		wantErr    bool
	}{
		{"nil series index", nil, Edits{}, false},
		{"with series in edits", nil, Edits{Series: new("New Series"), SeriesIndex: new("1")}, false},
		{"book has series, nil in edits", &model.SeriesRef{Name: "Existing"}, Edits{SeriesIndex: new("2.5")}, false},
		{"no series anywhere", nil, Edits{SeriesIndex: new("1")}, true},
		{"book has series, empty series edit", &model.SeriesRef{Name: "Existing"}, Edits{Series: new(string), SeriesIndex: new("1")}, false},

		// D.3.7's grammar: "A single xsd:unsignedInt or series of
		// decimal-separated numbers (e.g., 1 or 2.2.1)."
		{"multi-level index", &model.SeriesRef{Name: "Existing"}, Edits{SeriesIndex: new("2.2.1")}, false},
		{"empty index", &model.SeriesRef{Name: "Existing"}, Edits{SeriesIndex: new("")}, true},
		{"non-numeric index", &model.SeriesRef{Name: "Existing"}, Edits{SeriesIndex: new("two")}, true},
		{"negative index", &model.SeriesRef{Name: "Existing"}, Edits{SeriesIndex: new("-1")}, true},
		{"trailing separator", &model.SeriesRef{Name: "Existing"}, Edits{SeriesIndex: new("1.")}, true},
		{"float syntax we cannot store", &model.SeriesRef{Name: "Existing"}, Edits{SeriesIndex: new("1e3")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.edits, testBook("", tc.bookSeries))
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				assertHasFieldError(t, err, "series_index", "")
			}
		})
	}
}

func TestValidateMultipleErrors(t *testing.T) {
	e := Edits{
		Status: new("invalid"),
		Rating: new(-1.0),
		Title:  new(string),
	}
	err := Validate(e, testBook("", nil))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error is not *ValidationError: %T", err)
	}
	if len(*ve) != 3 {
		t.Errorf("expected 3 errors, got %d: %v", len(*ve), *ve)
	}
	fields := make(map[string]bool)
	for _, fe := range *ve {
		fields[fe.Field] = true
	}
	for _, f := range []string{"status", "rating", "title"} {
		if !fields[f] {
			t.Errorf("expected error for field %q", f)
		}
	}
}

func TestValidateNoErrors(t *testing.T) {
	e := Edits{
		Status:      new("reading"),
		Rating:      new(4.0),
		Title:       new("Valid Title"),
		Authors:     &[]model.Author{{Name: "Author"}},
		Language:    new("en"),
		Series:      new("New Series"),
		SeriesIndex: new("2"),
	}
	err := Validate(e, testBook("", nil))
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestFieldError(t *testing.T) {
	fe := FieldError{Field: "title", Message: "too short"}
	got := fe.Error()
	want := "title: too short"
	if got != want {
		t.Errorf("FieldError.Error() = %q, want %q", got, want)
	}
}

func TestValidationErrorEmpty(t *testing.T) {
	var ve ValidationError
	got := ve.Error()
	if got != "" {
		t.Errorf("ValidationError.Error() with 0 errors = %q, want empty", got)
	}
}

func TestValidationErrorSingle(t *testing.T) {
	ve := ValidationError{{Field: "title", Message: "too short"}}
	got := ve.Error()
	want := "title: too short"
	if got != want {
		t.Errorf("ValidationError.Error() with 1 error = %q, want %q", got, want)
	}
}

func TestValidationErrorMultiple(t *testing.T) {
	ve := ValidationError{
		{Field: "title", Message: "too short"},
		{Field: "rating", Message: "too high"},
	}
	got := ve.Error()
	for _, fe := range ve {
		if !strings.Contains(got, fe.Field+": "+fe.Message) {
			t.Errorf("ValidationError.Error() should contain %q", fe.Field+": "+fe.Message)
		}
	}
	if !strings.Contains(got, "; ") {
		t.Errorf("ValidationError.Error() with multiple errors should join with '; '")
	}
}

func TestValidateCoverNoCoverPath(t *testing.T) {
	e := Edits{Cover: new([]byte("image-data"))}
	err := Validate(e, testBook("", nil))
	if err == nil {
		t.Fatal("expected error: book has no cover to replace")
	}
	assertSingleFieldError(t, err, "cover")
}

func TestValidateCoverEmptyBytes(t *testing.T) {
	e := Edits{Cover: new([]byte{})}
	err := Validate(e, testBook("cover.jpg", nil))
	if err == nil {
		t.Fatal("expected error: empty cover bytes")
	}
}

func TestHasBibEdits(t *testing.T) {
	var empty Edits
	if empty.HasBibEdits() {
		t.Error("HasBibEdits should be false for empty Edits")
	}
	title := "x"
	if !(&Edits{Title: &title}).HasBibEdits() {
		t.Error("HasBibEdits should be true when Title is set")
	}
	series := "S"
	if !(&Edits{Series: &series}).HasBibEdits() {
		t.Error("HasBibEdits should be true when Series is set")
	}
}

func TestHasCoverEdit(t *testing.T) {
	var empty Edits
	if empty.HasCoverEdit() {
		t.Error("HasCoverEdit should be false for empty Edits")
	}
	cover := []byte{}
	if !(&Edits{Cover: &cover}).HasCoverEdit() {
		t.Error("HasCoverEdit should be true when Cover is set")
	}
}

func TestEditsNormalized(t *testing.T) {
	tests := []struct {
		name  string
		edits Edits
		// nil means the field must come back nil.
		wantRating *float64
		wantIndex  *string
	}{
		{"nil fields stay nil", Edits{}, nil, nil},

		// Ratings are stored to 2 decimal places.
		{"rating rounds down", Edits{Rating: new(4.564)}, new(4.56), nil},
		{"rating rounds up", Edits{Rating: new(4.567)}, new(4.57), nil},
		{"rating at the halfway point rounds away from zero", Edits{Rating: new(4.565)}, new(4.57), nil},
		{"rating already exact", Edits{Rating: new(4.5)}, new(4.5), nil},
		{"rating zero", Edits{Rating: new(0.0)}, new(0.0), nil},

		// The series index is a string carried from the epub and has no
		// precision to round to: D.3.7's "2.2.1" is not a number.
		{"index untouched", Edits{SeriesIndex: new("2.2.1")}, nil, new("2.2.1")},
		{"both fields", Edits{Rating: new(3.999), SeriesIndex: new("1.56")}, new(4.0), new("1.56")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.edits.Normalized()

			assertRounded(t, "Rating", got.Rating, tc.wantRating)
			if (got.SeriesIndex == nil) != (tc.wantIndex == nil) ||
				(tc.wantIndex != nil && *got.SeriesIndex != *tc.wantIndex) {
				t.Errorf("SeriesIndex = %v, want %v", got.SeriesIndex, tc.wantIndex)
			}
		})
	}
}

func assertRounded(t *testing.T, field string, got, want *float64) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Errorf("%s = %v, want nil — an absent edit must stay absent", field, *got)
	case want == nil:
	case got == nil:
		t.Errorf("%s = nil, want %v", field, *want)
	case *got != *want:
		t.Errorf("%s = %v, want %v", field, *got, *want)
	}
}

// TestEditsNormalizedKeepsNonFinite pins the interaction Normalized's doc
// comment relies on: rounding must leave NaN and ±Inf intact so Validate is
// still the thing that rejects them. Were rounding to fold them to a finite
// number, an unusable value would pass validation and reach the sidecar.
func TestEditsNormalizedKeepsNonFinite(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    float64
	}{
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Edits{Rating: new(tc.v)}.Normalized()

			if r := *got.Rating; !math.IsNaN(r) && !math.IsInf(r, 0) {
				t.Errorf("Rating = %v, want it left non-finite for Validate to reject", r)
			}
			// The pairing that matters: Validate still refuses it afterwards.
			if err := Validate(got, testBook("", &model.SeriesRef{Name: "S"})); err == nil {
				t.Error("Validate accepted a non-finite value that survived rounding")
			}
		})
	}
}

// TestEditsNormalizedDoesNotMutateItsReceiver: Edits is taken by value, but it
// carries pointers, so rewriting through them would reach back into the
// caller's copy.
func TestEditsNormalizedDoesNotMutateItsReceiver(t *testing.T) {
	rating := 4.567
	e := Edits{Rating: &rating}

	e.Normalized()

	if rating != 4.567 {
		t.Errorf("caller's Rating = %v, want it untouched at 4.567", rating)
	}
}
