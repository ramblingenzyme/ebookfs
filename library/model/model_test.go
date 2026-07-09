package model

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

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
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("error is not *ValidationError: %T", err)
				}
				if len(*ve) != 1 || (*ve)[0].Field != "status" {
					t.Errorf("expected status field error, got %v", *ve)
				}
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
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("error is not *ValidationError: %T", err)
				}
				if len(*ve) != 1 || (*ve)[0].Field != "rating" {
					t.Errorf("expected rating field error, got %v", *ve)
				}
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
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("error is not *ValidationError: %T", err)
				}
				if len(*ve) != 1 || (*ve)[0].Field != "title" {
					t.Errorf("expected title field error, got %v", *ve)
				}
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
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("error is not *ValidationError: %T", err)
				}
				found := false
				for _, fe := range *ve {
					if fe.Field == "authors" && strings.Contains(fe.Message, tc.errMsg) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected authors error containing %q, got %v", tc.errMsg, *ve)
				}
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
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("error is not *ValidationError: %T", err)
				}
				if len(*ve) != 1 || (*ve)[0].Field != "language" {
					t.Errorf("expected language field error, got %v", *ve)
				}
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
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("error is not *ValidationError: %T", err)
				}
				found := false
				for _, fe := range *ve {
					if fe.Field == "tags" {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected tags field error, got %v", *ve)
				}
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
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("error is not *ValidationError: %T", err)
				}
				found := false
				for _, fe := range *ve {
					if fe.Field == "series_index" {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected series_index error, got %v", *ve)
				}
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
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error is not *ValidationError: %T", err)
	}
	if len(*ve) != 1 || (*ve)[0].Field != "cover" {
		t.Errorf("expected cover field error, got %v", *ve)
	}
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
