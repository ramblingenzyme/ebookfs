package index

import (
	"slices"
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

// A filter is one Query field, answered both ways: where narrows a Search in
// SQL, matches tests a snapshot in Go. One type carries both, so a field cannot
// be added with only one side, and filter_test.go fails until Query.filters
// returns it.
//
// An empty filter imposes no constraint. where adds no condition and matches
// reports true; without that, Query{} would match nothing.
type filter interface {
	where(bq *bookQuery)
	matches(b *book.ImmutableBook) bool
}

// AuthorFilter matches a display name or a sort name, so a sort-name search
// finds the book filed under its display name.
type AuthorFilter []string

func (f AuthorFilter) where(bq *bookQuery) {
	addIn(bq, "b.id IN (SELECT ba.book_id FROM book_authors ba JOIN authors a ON a.id = ba.author_id WHERE a.name IN (%s) OR a.sort_name IN (%s))", f)
}

func (f AuthorFilter) matches(b *book.ImmutableBook) bool {
	return len(f) == 0 || slices.ContainsFunc(b.Authors(), func(a book.Author) bool {
		return slices.Contains(f, a.Name) || slices.Contains(f, a.SortName)
	})
}

// TagFilter matches an exact tag name.
type TagFilter []string

func (f TagFilter) where(bq *bookQuery) {
	addIn(bq, "b.id IN (SELECT bt.book_id FROM book_tags bt JOIN tags t ON t.id = bt.tag_id WHERE t.name IN (%s))", f)
}

func (f TagFilter) matches(b *book.ImmutableBook) bool {
	return len(f) == 0 || slices.ContainsFunc(f, func(t string) bool {
		return slices.Contains(b.Tags(), t)
	})
}

// SeriesFilter matches an exact series name.
type SeriesFilter []string

func (f SeriesFilter) where(bq *bookQuery) {
	addIn(bq, "b.series_id IN (SELECT id FROM series WHERE name IN (%s))", f)
}

func (f SeriesFilter) matches(b *book.ImmutableBook) bool {
	return len(f) == 0 || (b.HasSeries() && slices.Contains(f, b.SeriesName()))
}

// StatusFilter matches an exact reading status.
type StatusFilter []string

func (f StatusFilter) where(bq *bookQuery) { addIn(bq, "b.status IN (%s)", f) }

func (f StatusFilter) matches(b *book.ImmutableBook) bool {
	return len(f) == 0 || slices.Contains(f, b.Status())
}

// SubjectFilter matches an exact subject name.
type SubjectFilter []string

func (f SubjectFilter) where(bq *bookQuery) {
	addIn(bq, "b.id IN (SELECT bs.book_id FROM book_subjects bs JOIN subjects s ON s.id = bs.subject_id WHERE s.name IN (%s))", f)
}

func (f SubjectFilter) matches(b *book.ImmutableBook) bool {
	return len(f) == 0 || slices.ContainsFunc(f, func(s string) bool {
		return slices.Contains(b.Subjects(), s)
	})
}

// PublisherFilter matches an exact publisher name.
type PublisherFilter []string

func (f PublisherFilter) where(bq *bookQuery) { addIn(bq, "b.publisher IN (%s)", f) }

func (f PublisherFilter) matches(b *book.ImmutableBook) bool {
	return len(f) == 0 || slices.Contains(f, b.Publisher())
}

// LanguageFilter matches an exact language name.
type LanguageFilter []string

func (f LanguageFilter) where(bq *bookQuery) { addIn(bq, "b.language IN (%s)", f) }

func (f LanguageFilter) matches(b *book.ImmutableBook) bool {
	return len(f) == 0 || slices.Contains(f, b.Language())
}

type IDFilter []int64

func (f IDFilter) where(bq *bookQuery) { addIn(bq, "b.id IN (%s)", f) }

func (f IDFilter) matches(b *book.ImmutableBook) bool {
	return len(f) == 0 || slices.Contains(f, b.ID())
}

// TitleFilter matches a substring of the title, ASCII case-insensitive, unless
// Exact. Exact is binary equality, so it is case-sensitive.
//
// ponytail: SQLite's LIKE folds ASCII only, so title "émile" misses "Émile
// Zola" in SQL, though matches folds in Go and finds it. Store a title folded
// with strings.ToLower, match the folded term against it, and unskip the
// non-ASCII parity cases once such a search matters.
type TitleFilter struct {
	Terms []string
	Exact bool
}

func (f TitleFilter) where(bq *bookQuery) {
	if f.Exact {
		addIn(bq, "b.title IN (%s)", f.Terms)
		return
	}

	if len(f.Terms) == 0 {
		return
	}

	clauses := make([]string, len(f.Terms))
	args := make([]any, len(f.Terms))

	for i, t := range f.Terms {
		clauses[i] = "b.title LIKE ? ESCAPE '\\'"
		args[i] = "%" + likeEscaper.Replace(t) + "%"
	}

	bq.addCondition("("+strings.Join(clauses, " OR ")+")", args...)
}

func (f TitleFilter) matches(b *book.ImmutableBook) bool {
	if len(f.Terms) == 0 {
		return true
	}

	if f.Exact {
		return slices.Contains(f.Terms, b.Title())
	}

	lower := strings.ToLower(b.Title())

	return slices.ContainsFunc(f.Terms, func(term string) bool {
		return strings.Contains(lower, strings.ToLower(term))
	})
}

// likeEscaper escapes LIKE's wildcards for the ESCAPE '\' clause
// TitleFilter.where writes.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
