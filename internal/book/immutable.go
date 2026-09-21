package book

import (
	"maps"
	"slices"
	"time"
)

// ImmutableBook wraps a Book in read-only getters, so a caller outside this
// package cannot mutate library state. It is a snapshot; pkg/library.Library
// says when to fetch a fresh one.
type ImmutableBook struct {
	inner *Book
}

// NewImmutableBook wraps a Book. The caller must not retain or mutate b after
// passing it to this function.
func NewImmutableBook(b *Book) *ImmutableBook {
	return &ImmutableBook{inner: b}
}

func (b *ImmutableBook) ID() int64 { return b.inner.Meta.ID }

func (b *ImmutableBook) Title() string { return b.inner.Title }

func (b *ImmutableBook) SortTitle() string { return b.inner.SortTitle }

func (b *ImmutableBook) Authors() []Author { return slices.Clone(b.inner.Authors) }

func (b *ImmutableBook) Series() *SeriesRef {
	if b.inner.Series == nil {
		return nil
	}
	s := *b.inner.Series
	return &s
}

func (b *ImmutableBook) HasSeries() bool { return b.inner.HasSeries() }

func (b *ImmutableBook) SeriesName() string { return b.inner.SeriesName() }

func (b *ImmutableBook) SeriesIndex() string {
	if b.inner.Series == nil {
		return ""
	}
	return b.inner.Series.Index
}

// Language is a BCP 47 / ISO 639 code.
func (b *ImmutableBook) Language() string { return b.inner.Language }

func (b *ImmutableBook) Pubdate() string { return b.inner.Pubdate }

func (b *ImmutableBook) Description() string { return b.inner.Description }

func (b *ImmutableBook) Identifiers() map[string]string { return maps.Clone(b.inner.Identifiers) }

func (b *ImmutableBook) CoverPath() string { return b.inner.CoverPath }

func (b *ImmutableBook) OpfSize() int64 { return b.inner.OpfSize }

func (b *ImmutableBook) CoverSize() int64 { return b.inner.CoverSize }

func (b *ImmutableBook) EpubPath() string { return b.inner.EpubPath }

func (b *ImmutableBook) Dir() string { return b.inner.Dir() }

func (b *ImmutableBook) Filename() string { return b.inner.Filename() }

func (b *ImmutableBook) EpubSize() int64 { return b.inner.EpubSize }

func (b *ImmutableBook) DateAdded() time.Time { return b.inner.Meta.DateAdded }

func (b *ImmutableBook) DateModified() time.Time { return b.inner.Meta.DateModified }

func (b *ImmutableBook) Status() string { return b.inner.Meta.Status }

func (b *ImmutableBook) Rating() float64 { return b.inner.Meta.Rating }

func (b *ImmutableBook) Tags() []string { return slices.Clone(b.inner.Meta.Tags) }
