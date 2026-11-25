package book

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/fs/textfmt"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

type field struct {
	get func(*library.Book) string
	// edits converts string input to typed Edits. Error return is for input
	// parsing failures (e.g. strconv.Atoi); validation against the book's current
	// state is centralized in book.Validate, so this needs no snapshot.
	edits func(string) (library.Edits, error)
}

var fields = map[string]field{
	"status": {
		get: func(b *library.Book) string { return b.Status() },
		edits: func(s string) (library.Edits, error) {
			return library.Edits{Status: &s}, nil
		},
	},
	"rating": {
		get: func(b *library.Book) string { return strconv.FormatFloat(b.Rating(), 'f', -1, 64) },
		edits: func(s string) (library.Edits, error) {
			n, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return library.Edits{}, fmt.Errorf("invalid rating %q", s)
			}

			return library.Edits{Rating: &n}, nil
		},
	},
	"tags": {
		get: func(b *library.Book) string { return strings.Join(b.Tags(), "\n") },
		edits: func(s string) (library.Edits, error) {
			tags := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' })
			return library.Edits{Tags: &tags}, nil
		},
	},
	"title": {
		get: func(b *library.Book) string { return b.Title() },
		edits: func(s string) (library.Edits, error) {
			return library.Edits{Title: &s}, nil
		},
	},
	"language": {
		get: func(b *library.Book) string { return b.Language() },
		edits: func(s string) (library.Edits, error) {
			return library.Edits{Language: &s}, nil
		},
	},
	"description": {
		get: func(b *library.Book) string { return b.Description() },
		edits: func(s string) (library.Edits, error) {
			return library.Edits{Description: &s}, nil
		},
	},
	"publisher": {
		get: func(b *library.Book) string { return b.Publisher() },
		edits: func(s string) (library.Edits, error) {
			return library.Edits{Publisher: &s}, nil
		},
	},
	"rights": {
		get: func(b *library.Book) string { return b.Rights() },
		edits: func(s string) (library.Edits, error) {
			return library.Edits{Rights: &s}, nil
		},
	},
	"subjects": {
		get: func(b *library.Book) string { return strings.Join(b.Subjects(), "\n") },
		edits: func(s string) (library.Edits, error) {
			subjects := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' })
			return library.Edits{Subjects: &subjects}, nil
		},
	},
	"contributors": {
		get: func(b *library.Book) string {
			return joinLines(b.Contributors(), func(c library.Contributor) string {
				return textfmt.FormatPair(c.Name, c.Role)
			})
		},
		edits: func(s string) (library.Edits, error) {
			var contributors []library.Contributor

			for line := range strings.SplitSeq(s, "\n") {
				name, role := textfmt.ParsePair(line)
				if name == "" {
					continue
				}

				contributors = append(contributors, library.Contributor{Name: name, Role: role})
			}

			return library.Edits{Contributors: &contributors}, nil
		},
	},
	"authors": {
		get: func(b *library.Book) string {
			return joinLines(b.Authors(), func(a library.Author) string {
				return textfmt.FormatPair(a.Name, a.SortName)
			})
		},
		edits: func(s string) (library.Edits, error) {
			var authors []library.Author

			for line := range strings.SplitSeq(s, "\n") {
				a := textfmt.ParseAuthor(line)
				if a.Name == "" {
					continue
				}

				authors = append(authors, a)
			}

			if len(authors) == 0 {
				return library.Edits{}, fmt.Errorf("at least one author is required")
			}

			return library.Edits{Authors: &authors}, nil
		},
	},
	"series": {
		get: func(b *library.Book) string { return b.SeriesName() },
		edits: func(s string) (library.Edits, error) {
			return library.Edits{Series: &s}, nil
		},
	},
	"series_index": {
		get: func(b *library.Book) string {
			if !b.HasSeries() {
				return ""
			}

			return b.SeriesIndex()
		},
		// Passed through as written: the position is a string all the way from
		// the epub (EPUB 3.3 D.3.7 allows "2.2.1"), and its grammar is checked
		// by book.Validate along with every other field's.
		edits: func(s string) (library.Edits, error) {
			return library.Edits{SeriesIndex: &s}, nil
		},
	},
}

func joinLines[T any](xs []T, format func(T) string) string {
	lines := make([]string, len(xs))
	for i, x := range xs {
		lines[i] = format(x)
	}

	return strings.Join(lines, "\n")
}

// formatIdentifiers sorts by scheme, since sorting the "scheme=value" lines
// would put the ONIX scheme isbn-a before isbn.
func formatIdentifiers(ids map[string]string) string {
	return joinLines(slices.Sorted(maps.Keys(ids)), func(scheme string) string {
		return scheme + "=" + ids[scheme]
	})
}
