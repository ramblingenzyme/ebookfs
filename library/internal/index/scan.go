package index

import (
	"database/sql"
	"log/slog"
	"time"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// bookRow holds the raw scan targets for a book query row.
type bookRow struct {
	id, opfSize, coverSize, epubSize                                   int64
	title, pubdate, description, language, epubPath, coverPath, status string
	rating                                                             float64
	dateAdded, dateModified                                            string
	sortTitle                                                          sql.NullString
	seriesID                                                           sql.NullInt64
	seriesName                                                         sql.NullString
	seriesIndex                                                        sql.NullString
}

// toBook converts a bookRow to a model.Book.
func (r *bookRow) toBook() *model.Book {
	b := &model.Book{
		Meta: model.Meta{
			ID:           r.id,
			Status:       r.status,
			Rating:       r.rating,
			DateAdded:    parseDateField(r.dateAdded, "date_added", r.id),
			DateModified: parseDateField(r.dateModified, "date_modified", r.id),
		},
		Bib: model.Bib{
			Title:       r.title,
			SortTitle:   r.sortTitle.String,
			Pubdate:     r.pubdate,
			Description: r.description,
			Language:    r.language,
			CoverPath:   r.coverPath,
			OpfSize:     r.opfSize,
			CoverSize:   r.coverSize,
		},
		Location: model.Location{
			EpubPath: r.epubPath,
		},
		EpubSize: r.epubSize,
	}
	if r.seriesName.Valid {
		b.Series = &model.SeriesRef{
			ID:    r.seriesID.Int64,
			Name:  r.seriesName.String,
			Index: r.seriesIndex.String,
		}
	}
	return b
}

// parseDateField parses an RFC3339 date string, logging a warning on failure.
func parseDateField(s string, field string, bookID int64) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err != nil {
		slog.Warn("invalid "+field, "book_id", bookID, field, s, "error", err)
		return time.Time{}
	} else {
		return t
	}
}

// scanBookRows scans the main query rows into Book records.
func scanBookRows(rows *sql.Rows) ([]*model.Book, error) {
	var books []*model.Book
	for rows.Next() {
		var r bookRow
		if err := rows.Scan(
			&r.id, &r.title, &r.sortTitle, &r.pubdate, &r.description, &r.language,
			&r.epubPath, &r.coverPath,
			&r.status, &r.rating, &r.dateAdded, &r.dateModified,
			&r.seriesID, &r.seriesName, &r.seriesIndex,
			&r.opfSize, &r.coverSize, &r.epubSize,
		); err != nil {
			return nil, err
		}
		books = append(books, r.toBook())
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return books, nil
}
