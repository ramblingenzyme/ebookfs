package index

import (
	"database/sql"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/library/internal/index/dbsqlc"
)

// toUnixNano encodes an mtime for storage. The zero time means "never observed"
// and stores as 0 — note time.Time{}.UnixNano() is a large negative number, so
// the zero case has to be handled explicitly.
//
// We ignore the edge case of a file having a mtime of 0 because that would imply
// a broken system clock, rather than a real value
func toUnixNano(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

// fromUnixNano decodes a stored mtime, mapping 0 back to the zero time. A zero
// time never equals a real file's mtime, so an unrecorded value reads as drift.
func fromUnixNano(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

// toNullString converts a string to a sql.NullString, setting Valid to true
// only when the string is non-empty.
func toNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// insertBook inserts a new book row, failing on id conflict — used by Rebuild.
//
// It deliberately skips cleanupOrphans: Rebuild empties every table before the
// insert loop, so each author/series/tag is written alongside the book that
// references it and nothing can be orphaned. Sweeping per book would run three
// growing anti-join scans N times for no effect.
func (idx *Index) insertBook(q *dbsqlc.Queries, b *book.Book, mt drift.PathInfo) error {
	sortTitle := toNullString(b.SortTitle)
	pubdate := toNullString(b.Pubdate)

	err := q.InsertBook(idx.ctx, dbsqlc.InsertBookParams{
		ID:           b.Meta.ID,
		Title:        b.Title,
		SortTitle:    sortTitle,
		Pubdate:      pubdate,
		Description:  b.Description,
		Language:     b.Language,
		EpubPath:     b.EpubPath,
		CoverPath:    b.CoverPath,
		Status:       b.Meta.Status,
		Rating:       b.Meta.Rating,
		DateAdded:    b.Meta.DateAdded.UTC().Format(time.RFC3339),
		DateModified: b.Meta.DateModified.UTC().Format(time.RFC3339),
		SeriesID:     sql.NullInt64{},  // series_id set by finishBook
		SeriesIndex:  sql.NullString{}, // series_index set by finishBook
		OpfSize:      b.OpfSize,
		CoverSize:    b.CoverSize,
		EpubSize:     mt.Size,
		EpubMtime:    toUnixNano(mt.EpubMtime),
		MetaMtime:    toUnixNano(mt.MetaMtime),
		MetaSize:     mt.MetaSize,
	})
	if err != nil {
		return err
	}

	return idx.finishBook(q, b)
}

// putBook inserts or replaces b, using ON CONFLICT to update an existing row.
// Rebuild, which must surface id collisions, uses insertBook instead.
func (idx *Index) putBook(q *dbsqlc.Queries, b *book.Book, mt drift.PathInfo) error {
	sortTitle := toNullString(b.SortTitle)
	pubdate := toNullString(b.Pubdate)

	err := q.UpsertBook(idx.ctx, dbsqlc.UpsertBookParams{
		ID:           b.Meta.ID,
		Title:        b.Title,
		SortTitle:    sortTitle,
		Pubdate:      pubdate,
		Description:  b.Description,
		Language:     b.Language,
		EpubPath:     b.EpubPath,
		CoverPath:    b.CoverPath,
		Status:       b.Meta.Status,
		Rating:       b.Meta.Rating,
		DateAdded:    b.Meta.DateAdded.UTC().Format(time.RFC3339),
		DateModified: b.Meta.DateModified.UTC().Format(time.RFC3339),
		SeriesID:     sql.NullInt64{},  // series_id set by finishBook
		SeriesIndex:  sql.NullString{}, // series_index set by finishBook
		OpfSize:      b.OpfSize,
		CoverSize:    b.CoverSize,
		EpubSize:     mt.Size,
		EpubMtime:    toUnixNano(mt.EpubMtime),
		MetaMtime:    toUnixNano(mt.MetaMtime),
		MetaSize:     mt.MetaSize,
	})
	if err != nil {
		return err
	}

	if err := idx.finishBook(q, b); err != nil {
		return err
	}
	// Replacing a book can strand its former author/series/tag rows.
	return idx.cleanupOrphans(q)
}

// finishBook writes a book's authors, tags, series, and identifiers. It does not
// sweep orphans — callers that can strand rows (putBook, deleteBook) call
// cleanupOrphans themselves.
func (idx *Index) finishBook(q *dbsqlc.Queries, b *book.Book) error {
	if err := idx.upsertAuthors(q, b.Meta.ID, b.Authors); err != nil {
		return err
	}
	if err := idx.upsertTags(q, b.Meta.ID, b.Meta.Tags); err != nil {
		return err
	}
	if err := idx.upsertSeries(q, b); err != nil {
		return err
	}

	if err := q.DeleteBookIdentifiers(idx.ctx, b.Meta.ID); err != nil {
		return err
	}
	for scheme, value := range b.Identifiers {
		if err := q.InsertIdentifier(idx.ctx, dbsqlc.InsertIdentifierParams{
			BookID: b.Meta.ID,
			Scheme: scheme,
			Value:  value,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (idx *Index) upsertAuthors(q *dbsqlc.Queries, bookID int64, authors []book.Author) error {
	if err := q.DeleteBookAuthors(idx.ctx, bookID); err != nil {
		return err
	}
	for i, a := range authors {
		// Insert or update: only overwrite sort_name when we have a real value and the
		// stored one is empty (fills in missing file-as data without stomping corrections).
		if err := q.InsertAuthor(idx.ctx, dbsqlc.InsertAuthorParams{
			Name:     a.Name,
			SortName: a.SortName,
		}); err != nil {
			return err
		}
		author, err := q.GetAuthorByName(idx.ctx, a.Name)
		if err != nil {
			return err
		}
		if err := q.InsertBookAuthor(idx.ctx, dbsqlc.InsertBookAuthorParams{
			BookID:   bookID,
			AuthorID: author.ID,
			Position: int64(i),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (idx *Index) upsertTags(q *dbsqlc.Queries, bookID int64, tags []string) error {
	if err := q.DeleteBookTags(idx.ctx, bookID); err != nil {
		return err
	}
	for _, tag := range tags {
		if err := q.InsertTag(idx.ctx, tag); err != nil {
			return err
		}
		tagRow, err := q.GetTagByName(idx.ctx, tag)
		if err != nil {
			return err
		}
		if err := q.InsertBookTag(idx.ctx, dbsqlc.InsertBookTagParams{
			BookID: bookID,
			TagID:  tagRow.ID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// upsertSeries points the book at its series or clears series_id, then removes
// orphaned series rows. It must run after the books row exists.
func (idx *Index) upsertSeries(q *dbsqlc.Queries, b *book.Book) error {
	var seriesID sql.NullInt64
	var seriesIndex sql.NullString

	if b.Series != nil {
		if err := q.InsertSeries(idx.ctx, b.Series.Name); err != nil {
			return err
		}
		series, err := q.GetSeriesByName(idx.ctx, b.Series.Name)
		if err != nil {
			return err
		}
		seriesID = sql.NullInt64{Int64: series.ID, Valid: true}
		seriesIndex = sql.NullString{String: b.Series.Index, Valid: true}
	}

	if err := q.UpdateBookSeries(idx.ctx, dbsqlc.UpdateBookSeriesParams{
		SeriesID:    seriesID,
		SeriesIndex: seriesIndex,
		ID:          b.Meta.ID,
	}); err != nil {
		return err
	}

	return nil
}

func (idx *Index) deleteBook(q *dbsqlc.Queries, id int64) error {
	// ON DELETE CASCADE handles book_authors, book_tags, identifiers.
	if err := q.DeleteBook(idx.ctx, id); err != nil {
		return err
	}
	return idx.cleanupOrphans(q)
}

// cleanupOrphans removes authors, series, and tags that are no longer
// referenced by any book.
func (idx *Index) cleanupOrphans(q *dbsqlc.Queries) error {
	if err := q.DeleteOrphanedAuthors(idx.ctx); err != nil {
		return err
	}
	if err := q.DeleteOrphanedSeries(idx.ctx); err != nil {
		return err
	}
	if err := q.DeleteOrphanedTags(idx.ctx); err != nil {
		return err
	}
	return nil
}
