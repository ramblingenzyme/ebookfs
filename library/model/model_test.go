package model

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

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
	book := &Book{}
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
			e := Edits{Status: ptr(tc.status)}
			err := e.Validate(book)
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
	book := &Book{}
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
			e := Edits{Rating: ptr(tc.rating)}
			err := e.Validate(book)
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
	book := &Book{}
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
			e := Edits{Title: ptr(tc.title)}
			err := e.Validate(book)
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
	book := &Book{}
	for _, tc := range []struct {
		name    string
		authors *[]Author
		wantErr bool
		errMsg  string
	}{
		{"nil untouched", nil, false, ""},
		{"valid single", &[]Author{{Name: "Alice"}}, false, ""},
		{"valid multiple", &[]Author{{Name: "Alice"}, {Name: "Bob"}}, false, ""},
		{"empty slice", &[]Author{}, true, "at least one author"},
		{"empty name", &[]Author{{Name: ""}}, true, "author 1 has an empty name"},
		{"second empty name", &[]Author{{Name: "Alice"}, {Name: ""}}, true, "author 2 has an empty name"},
		{"whitespace name", &[]Author{{Name: "   "}}, true, "author 1 has an empty name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := Edits{Authors: tc.authors}
			err := e.Validate(book)
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
	book := &Book{}
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
			e := Edits{Language: ptr(tc.lang)}
			err := e.Validate(book)
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
	book := &Book{}
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
			err := e.Validate(book)
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
		bookSeries *SeriesRef
		edits      Edits
		wantErr    bool
	}{
		{"nil series index", nil, Edits{}, false},
		{"with series in edits", nil, Edits{Series: ptr("New Series"), SeriesIndex: ptr(1.0)}, false},
		{"book has series, nil in edits", &SeriesRef{Name: "Existing"}, Edits{SeriesIndex: ptr(2.5)}, false},
		{"no series anywhere", nil, Edits{SeriesIndex: ptr(1.0)}, true},
		{"book has series, empty series edit", &SeriesRef{Name: "Existing"}, Edits{Series: ptr(""), SeriesIndex: ptr(1.0)}, false},
		{"NaN index", &SeriesRef{Name: "Existing"}, Edits{SeriesIndex: ptr(math.NaN())}, true},
		{"infinite index", &SeriesRef{Name: "Existing"}, Edits{SeriesIndex: ptr(math.Inf(1))}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			book := &Book{Bib: Bib{Series: tc.bookSeries}}
			err := tc.edits.Validate(book)
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
	book := &Book{}
	e := Edits{
		Status: ptr("invalid"),
		Rating: ptr(-1.0),
		Title:  ptr(""),
	}
	err := e.Validate(book)
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
	book := &Book{Bib: Bib{Series: &SeriesRef{Name: "Series"}}}
	e := Edits{
		Status:      ptr("reading"),
		Rating:      ptr(4.0),
		Title:       ptr("Valid Title"),
		Authors:     &[]Author{{Name: "Author"}},
		Language:    ptr("en"),
		Series:      ptr("New Series"),
		SeriesIndex: ptr(2.0),
	}
	err := e.Validate(book)
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
	book := &Book{}
	e := Edits{Cover: ptr([]byte("image-data"))}
	err := e.Validate(book)
	if err == nil {
		t.Fatal("expected error: book has no cover to replace")
	}
	assertSingleFieldError(t, err, "cover")
}

func TestValidateCoverEmptyBytes(t *testing.T) {
	book := &Book{Bib: Bib{CoverPath: "cover.jpg"}}
	e := Edits{Cover: ptr([]byte{})}
	err := e.Validate(book)
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

func TestJoinAuthors(t *testing.T) {
	tests := []struct {
		name    string
		authors []Author
		sep     string
		want    string
	}{
		{"single", []Author{{Name: "Alice"}}, ", ", "Alice"},
		{"multiple", []Author{{Name: "Alice"}, {Name: "Bob"}}, ", ", "Alice, Bob"},
		{"directory separator", []Author{{Name: "Alice"}, {Name: "Bob"}}, " & ", "Alice & Bob"},
		{"empty names", []Author{{Name: ""}, {Name: ""}}, ", ", UnknownAuthor},
		{"mixed empty and valid", []Author{{Name: ""}, {Name: "Alice"}}, ", ", "Alice"},
		{"nil", nil, ", ", UnknownAuthor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := JoinAuthors(tt.authors, tt.sep)
			if got != tt.want {
				t.Errorf("JoinAuthors(%v, %q) = %q, want %q", tt.authors, tt.sep, got, tt.want)
			}
		})
	}
}

// TestEditsNormalized pins the rounding policy. It is applied once, by
// Library.Edit, precisely so no frontend re-implements it — which also means
// nothing else would notice if it stopped happening.
func TestEditsNormalized(t *testing.T) {
	tests := []struct {
		name  string
		edits Edits
		// nil means the field must come back nil.
		wantRating *float64
		wantIndex  *float64
	}{
		{"nil fields stay nil", Edits{}, nil, nil},

		// Ratings are stored to 2 decimal places.
		{"rating rounds down", Edits{Rating: ptr(4.564)}, ptr(4.56), nil},
		{"rating rounds up", Edits{Rating: ptr(4.567)}, ptr(4.57), nil},
		{"rating at the halfway point rounds away from zero", Edits{Rating: ptr(4.565)}, ptr(4.57), nil},
		{"rating already exact", Edits{Rating: ptr(4.5)}, ptr(4.5), nil},
		{"rating zero", Edits{Rating: ptr(0.0)}, ptr(0.0), nil},

		// Series indices are stored to 1 decimal place.
		{"index rounds down", Edits{SeriesIndex: ptr(1.54)}, nil, ptr(1.5)},
		{"index rounds up", Edits{SeriesIndex: ptr(1.56)}, nil, ptr(1.6)},
		{"index already exact", Edits{SeriesIndex: ptr(2.0)}, nil, ptr(2.0)},

		{"both fields", Edits{Rating: ptr(3.999), SeriesIndex: ptr(0.04)}, ptr(4.0), ptr(0.0)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.edits.Normalized()

			assertRounded(t, "Rating", got.Rating, tc.wantRating)
			assertRounded(t, "SeriesIndex", got.SeriesIndex, tc.wantIndex)
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
			got := Edits{Rating: ptr(tc.v), SeriesIndex: ptr(tc.v)}.Normalized()

			if r := *got.Rating; !math.IsNaN(r) && !math.IsInf(r, 0) {
				t.Errorf("Rating = %v, want it left non-finite for Validate to reject", r)
			}
			if i := *got.SeriesIndex; !math.IsNaN(i) && !math.IsInf(i, 0) {
				t.Errorf("SeriesIndex = %v, want it left non-finite for Validate to reject", i)
			}
			// The pairing that matters: Validate still refuses it afterwards.
			book := &Book{Bib: Bib{Series: &SeriesRef{Name: "S"}}}
			if err := got.Validate(book); err == nil {
				t.Error("Validate accepted a non-finite value that survived rounding")
			}
		})
	}
}

// TestEditsNormalizedDoesNotMutateItsReceiver: Edits is taken by value, but it
// carries pointers, so rewriting through them would reach back into the
// caller's copy.
func TestEditsNormalizedDoesNotMutateItsReceiver(t *testing.T) {
	rating, index := 4.567, 1.56
	e := Edits{Rating: &rating, SeriesIndex: &index}

	e.Normalized()

	if rating != 4.567 {
		t.Errorf("caller's Rating = %v, want it untouched at 4.567", rating)
	}
	if index != 1.56 {
		t.Errorf("caller's SeriesIndex = %v, want it untouched at 1.56", index)
	}
}
