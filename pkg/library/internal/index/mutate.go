package index

import (
	"database/sql"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index/dbsqlc"
)

// toUnixNano stores the zero time as 0, handled explicitly because
// time.Time{}.UnixNano() is a large negative number. A real file dated 0
// implies a broken clock, so the collision is ignored.
func toUnixNano(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

func fromUnixNano(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

func toNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// insertBook fails on an id conflict, and is what Rebuild uses. It skips
// cleanupOrphans, since Rebuild empties every table first, so nothing can be
// orphaned and sweeping per book would run three growing anti-join scans N
// times for no effect.
func (idx *Index) insertBook(q *dbsqlc.Queries, b *book.Book, mt drift.PathInfo) error {
	if err := q.InsertBook(idx.ctx, bookParams(b, mt)); err != nil {
		return err
	}

	return idx.finishBook(q, b)
}

// putBook inserts or replaces b; insertBook says why Rebuild uses that one.
func (idx *Index) putBook(q *dbsqlc.Queries, b *book.Book, mt drift.PathInfo) error {
	// The two generated param types are field-identical, so the row is built once
	// and converted. A regenerate that makes them diverge fails to compile here
	// rather than letting the two writers drift apart.
	if err := q.UpsertBook(idx.ctx, dbsqlc.UpsertBookParams(bookParams(b, mt))); err != nil {
		return err
	}

	if err := idx.finishBook(q, b); err != nil {
		return err
	}
	// Replacing a book can strand its former author/series/tag rows.
	return idx.cleanupOrphans(q)
}

// bookParams is the index row for b: every column except series_id and
// series_index, which finishBook sets once the series row exists.
func bookParams(b *book.Book, mt drift.PathInfo) dbsqlc.InsertBookParams {
	return dbsqlc.InsertBookParams{
		ID:           b.Meta.ID,
		Title:        b.Title,
		SortTitle:    toNullString(b.SortTitle),
		Pubdate:      toNullString(b.Pubdate),
		Description:  b.Description,
		Language:     b.Language,
		EpubPath:     b.EpubPath,
		CoverPath:    b.CoverPath,
		Status:       b.Meta.Status,
		Rating:       b.Meta.Rating,
		DateAdded:    b.Meta.DateAdded.UTC().Format(time.RFC3339),
		DateModified: b.Meta.DateModified.UTC().Format(time.RFC3339),
		SeriesID:     sql.NullInt64{},
		SeriesIndex:  sql.NullString{},
		OpfSize:      b.OpfSize,
		CoverSize:    b.CoverSize,
		EpubSize:     mt.Size,
		EpubMtime:    toUnixNano(mt.EpubMtime),
		MetaMtime:    toUnixNano(mt.MetaMtime),
		MetaSize:     mt.MetaSize,
	}
}

// finishBook sweeps no orphans. The callers that can strand rows, putBook and
// deleteBook, call cleanupOrphans themselves.
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
		// Overwrites sort_name only with a real value onto an empty one, so a
		// missing file-as is filled in and a correction is not stomped.
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

// upsertSeries runs after the books row exists, since it updates that row.
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
