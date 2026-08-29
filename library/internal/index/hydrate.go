package index

import (
	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// hydrateBooks loads authors, tags, and identifiers for the given books via
// batch queries, assigning them in place.
func (idx *Index) hydrateBooks(books []*book.Book) error {
	ids := make([]int64, len(books))
	for i, b := range books {
		ids[i] = b.Meta.ID
	}

	authors, err := idx.loadAuthors(ids)
	if err != nil {
		return err
	}
	tags, err := idx.loadTags(ids)
	if err != nil {
		return err
	}
	identifiers, err := idx.loadIdentifiers(ids)
	if err != nil {
		return err
	}

	for _, b := range books {
		b.Authors = authors[b.Meta.ID]
		if b.Authors == nil {
			b.Authors = []model.Author{}
		}
		b.Meta.Tags = tags[b.Meta.ID]
		if b.Meta.Tags == nil {
			b.Meta.Tags = []string{}
		}
		if m := identifiers[b.Meta.ID]; m != nil {
			b.Identifiers = m
		} else {
			b.Identifiers = make(map[string]string)
		}
	}

	return nil
}

// loadAuthors fetches authors for the given book IDs, grouped by book.
func (idx *Index) loadAuthors(ids []int64) (map[int64][]model.Author, error) {
	rows, err := idx.queries.GetAuthorsByBookIDs(idx.ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]model.Author, len(ids))
	for _, row := range rows {
		out[row.BookID] = append(out[row.BookID], model.Author{
			ID:       row.ID,
			Name:     row.Name,
			SortName: row.SortName,
		})
	}
	return out, nil
}

// loadTags fetches tags for the given book IDs, grouped by book.
func (idx *Index) loadTags(ids []int64) (map[int64][]string, error) {
	rows, err := idx.queries.GetTagsByBookIDs(idx.ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]string, len(ids))
	for _, row := range rows {
		out[row.BookID] = append(out[row.BookID], row.Name)
	}
	return out, nil
}

// loadIdentifiers fetches identifiers for the given book IDs, grouped by book.
func (idx *Index) loadIdentifiers(ids []int64) (map[int64]map[string]string, error) {
	rows, err := idx.queries.GetIdentifiersByBookIDs(idx.ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]map[string]string, len(ids))
	for _, row := range rows {
		m := out[row.BookID]
		if m == nil {
			m = make(map[string]string)
			out[row.BookID] = m
		}
		m[row.Scheme] = row.Value
	}
	return out, nil
}
