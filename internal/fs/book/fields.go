package book

import (
	"fmt"
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
	// state is centralized in edits.Validate, so this needs no snapshot.
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
	"authors": {
		get: func(b *library.Book) string {
			authors := b.Authors()
			lines := make([]string, len(authors))
			for i, a := range authors {
				if a.SortName != "" {
					lines[i] = fmt.Sprintf("%s | %s", a.Name, a.SortName)
				} else {
					lines[i] = a.Name
				}
			}
			return strings.Join(lines, "\n")
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
		// by edits.Validate along with every other field's.
		edits: func(s string) (library.Edits, error) {
			return library.Edits{SeriesIndex: &s}, nil
		},
	},
}

// formatIdentifiers renders the identifier map as "scheme=value" lines, sorted
// by scheme because a map has no order and a file that shuffles between reads is
// no use to a diff or a script. Read-only, so nothing parses this back.
//
// The schemes are sorted, not the rendered lines: "=" sorts after "-", so a line
// sort puts isbn-a before isbn and orders the file by something no reader would
// guess. Both schemes come out of one ONIX code list, so the pair is reachable.
func formatIdentifiers(ids map[string]string) string {
	schemes := make([]string, 0, len(ids))
	for scheme := range ids {
		schemes = append(schemes, scheme)
	}
	slices.Sort(schemes)

	lines := make([]string, 0, len(schemes))
	for _, scheme := range schemes {
		lines = append(lines, scheme+"="+ids[scheme])
	}
	return strings.Join(lines, "\n")
}
