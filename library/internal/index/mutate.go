package index

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// PathInfo carries the on-disk state that the library layer observes with one
// stat per file and the index stores for drift detection. Size accompanies the
// mtimes because mtime alone cannot detect a change made within the same clock
// tick as the recorded one — filesystems that stamp mtimes from the kernel's
// coarse clock (tmpfs among them) hand out identical nanosecond values for
// writes in the same tick. All of it is internal bookkeeping, not exposed on
// model.Book.
type PathInfo struct {
	// EpubFilename is the epub's name within the book directory. Renaming a
	// file preserves its size and mtime, so without the name a rename is
	// invisible to drift detection and the index keeps serving a path that no
	// longer exists. For an indexed book it is not persisted here — AllPathInfo
	// reads books.epub_filename, the row's own copy — so Put ignores it; only
	// skipped_books, which has no book row, stores it.
	EpubFilename string
	Size         int64 // epub size, from the same stat as EpubMtime
	EpubMtime    time.Time
	MetaSize     int64 // meta.toml size, from the same stat as MetaMtime
	MetaMtime    time.Time
}

// Unobserved returns the state recorded for a book directory whose files could
// not be stat'd. It is a definite value rather than an absent one, so both
// sides of drift detection can record "we looked and could not see it" and
// agree with each other across restarts — otherwise one unreadable book means a
// full reindex on every startup, forever. The epub's name is still carried, so
// the directory is not mistaken for a different one; if the files become
// readable again the observed state differs from this and the book earns
// another indexing attempt.
func Unobserved(epubFilename string) PathInfo {
	return PathInfo{EpubFilename: epubFilename}
}

// IsUnobserved reports whether p records a failed observation rather than a
// real one. No stat of an existing file yields two zero mtimes.
func (p PathInfo) IsUnobserved() bool {
	return p.EpubMtime.IsZero() && p.MetaMtime.IsZero()
}

// Equal reports whether two observations describe the same on-disk state. The
// times need Time.Equal rather than ==, which also compares location and
// monotonic reading.
func (p PathInfo) Equal(o PathInfo) bool {
	return p.EpubFilename == o.EpubFilename &&
		p.Size == o.Size && p.MetaSize == o.MetaSize &&
		p.EpubMtime.Equal(o.EpubMtime) &&
		p.MetaMtime.Equal(o.MetaMtime)
}

// toUnixNano encodes an mtime for storage. The zero time means "never observed"
// and stores as 0 — note time.Time{}.UnixNano() is a large negative number, so
// the zero case has to be handled explicitly.
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

// pathInfoColumns are the drift-bookkeeping columns, in the order
// pathInfoValues supplies them. books carries them after its own columns;
// skipped_books carries them alone, keyed by library_path. Stating the tuple
// once keeps the two tables and every statement that reads them in step.
const pathInfoColumns = `epub_stat_size, epub_mtime, meta_mtime, meta_stat_size`

// pathInfoSelect reads a full PathInfo, keyed by library path. epub_filename is
// deliberately not in pathInfoColumns: that list is spliced into bookColumns,
// and books already carries epub_filename as the book's own location — a second
// copy would be the epub_stat_size/epub_size trap again. Both tables expose it
// under the same name, so the read path can still state the tuple once.
const pathInfoSelect = `library_path, epub_filename, ` + pathInfoColumns

// pathInfoValues returns mt's columns in pathInfoColumns order.
func pathInfoValues(mt PathInfo) []any {
	return []any{mt.Size, toUnixNano(mt.EpubMtime), toUnixNano(mt.MetaMtime), mt.MetaSize}
}

// pathInfoUpdates is the ON CONFLICT DO UPDATE clause for pathInfoColumns,
// derived once at init rather than per statement execution.
var pathInfoUpdates = excludedAssignments(pathInfoColumns)

// excludedAssignments renders `col=excluded.col, …` for an ON CONFLICT DO
// UPDATE clause, so a column list drives its own update rather than being
// restated by hand.
func excludedAssignments(columns string) string {
	parts := strings.Split(columns, ",")
	for i, c := range parts {
		c = strings.TrimSpace(c)
		parts[i] = c + "=excluded." + c
	}
	return strings.Join(parts, ", ")
}

const bookColumns = `(id, title, sort_title, pubdate, description, language,
		     library_path, epub_filename, cover_path, status, rating,
		     date_added, date_modified, opf_size, cover_size, epub_size,
		     ` + pathInfoColumns + `)`

// bookPlaceholders is the VALUES list for bookColumns, derived from it so a
// new column cannot leave the two disagreeing on a hand-counted run of `?`.
var bookPlaceholders = "(?" + strings.Repeat(", ?", strings.Count(bookColumns, ",")) + ")"

func bookValues(b *model.Book, mt PathInfo) []any {
	sortTitle := any(b.SortTitle)
	if sortTitle == "" {
		sortTitle = nil
	}
	return append([]any{
		b.Meta.ID, b.Title, sortTitle, b.Pubdate, b.Description, b.Language,
		b.LibraryPath, b.EpubFilename, b.CoverPath, b.Meta.Status, b.Meta.Rating,
		b.Meta.DateAdded.UTC().Format(time.RFC3339),
		b.Meta.DateModified.UTC().Format(time.RFC3339),
		b.OpfSize, b.CoverSize, b.EpubSize,
	}, pathInfoValues(mt)...)
}

// insertBook inserts a new book row, failing on id conflict — used by Rebuild.
//
// It deliberately skips cleanupOrphans: Rebuild empties every table before the
// insert loop, so each author/series/tag is written alongside the book that
// references it and nothing can be orphaned. Sweeping per book would run three
// growing anti-join scans N times for no effect.
func insertBook(tx *sql.Tx, b *model.Book, mt PathInfo) error {
	// series_id/series_index are set by finishBook's upsertSeries.
	if _, err := tx.Exec(
		`INSERT INTO books `+bookColumns+` VALUES `+bookPlaceholders,
		bookValues(b, mt)...,
	); err != nil {
		return err
	}

	return finishBook(tx, b)
}

// putBook inserts or replaces b, using ON CONFLICT to update an existing row.
// Rebuild, which must surface id collisions, uses insertBook instead.
func putBook(tx *sql.Tx, b *model.Book, mt PathInfo) error {
	// series_id/series_index are set by finishBook's upsertSeries.
	if _, err := tx.Exec(
		`INSERT INTO books `+bookColumns+` VALUES `+bookPlaceholders+`
		 ON CONFLICT(id) DO UPDATE SET
		     title=excluded.title, sort_title=excluded.sort_title, pubdate=excluded.pubdate,
		     description=excluded.description, language=excluded.language,
		     library_path=excluded.library_path, epub_filename=excluded.epub_filename,
		     cover_path=excluded.cover_path, status=excluded.status, rating=excluded.rating,
		     date_added=excluded.date_added, date_modified=excluded.date_modified,
		     opf_size=excluded.opf_size, cover_size=excluded.cover_size, epub_size=excluded.epub_size,
		     `+pathInfoUpdates,
		bookValues(b, mt)...,
	); err != nil {
		return err
	}

	if err := finishBook(tx, b); err != nil {
		return err
	}
	// Replacing a book can strand its former author/series/tag rows.
	return cleanupOrphans(tx)
}

// finish runs fn and clears the pending row in one transaction. MarkPending
// must have been called first so a pending row protects the preceding store
// writes; the row is atomically deleted inside the same transaction.
func (o *Op) finish(fn func(*sql.Tx) error) error {
	if o.opID == "" {
		return errors.New("MarkPending must be called before commit")
	}
	return o.idx.withTx(func(tx *sql.Tx) error {
		if err := fn(tx); err != nil {
			return err
		}
		_, err := tx.Exec("DELETE FROM pending_ops WHERE op_id = ?", o.opID)
		return err
	})
}

// Put writes b into the index, inserting or replacing the record for b.Meta.ID.
// mt carries the on-disk file state used for drift detection.
func (o *Op) Put(b *model.Book, mt PathInfo) error {
	return o.finish(func(tx *sql.Tx) error { return putBook(tx, b, mt) })
}

// Delete removes all index rows for book.
func (o *Op) Delete(bookID int64) error {
	return o.finish(func(tx *sql.Tx) error { return deleteBook(tx, bookID) })
}

// finishBook writes a book's authors, tags, series, and identifiers. It does not
// sweep orphans — callers that can strand rows (putBook, deleteBook) call
// cleanupOrphans themselves.
func finishBook(tx *sql.Tx, b *model.Book) error {
	if err := upsertAuthors(tx, b.Meta.ID, b.Authors); err != nil {
		return err
	}
	if err := upsertTags(tx, b.Meta.ID, b.Meta.Tags); err != nil {
		return err
	}
	if err := upsertSeries(tx, b); err != nil {
		return err
	}

	if _, err := tx.Exec(`DELETE FROM identifiers WHERE book_id=?`, b.Meta.ID); err != nil {
		return err
	}
	for scheme, value := range b.Identifiers {
		if _, err := tx.Exec(
			`INSERT INTO identifiers (book_id, scheme, value) VALUES (?, ?, ?)`,
			b.Meta.ID, scheme, value,
		); err != nil {
			return err
		}
	}
	return nil
}

func upsertAuthors(tx *sql.Tx, bookID int64, authors []model.Author) error {
	if _, err := tx.Exec(`DELETE FROM book_authors WHERE book_id=?`, bookID); err != nil {
		return err
	}
	for i, a := range authors {
		// Insert or update: only overwrite sort_name when we have a real value and the
		// stored one is empty (fills in missing file-as data without stomping corrections).
		if _, err := tx.Exec(
			`INSERT INTO authors (name, sort_name) VALUES (?, ?)
			 ON CONFLICT(name) DO UPDATE SET sort_name=excluded.sort_name
			 WHERE excluded.sort_name != '' AND authors.sort_name = ''`,
			a.Name, a.SortName,
		); err != nil {
			return err
		}
		var authorID int64
		if err := tx.QueryRow(`SELECT id FROM authors WHERE name=?`, a.Name).Scan(&authorID); err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO book_authors (book_id, author_id, position) VALUES (?, ?, ?)`,
			bookID, authorID, i,
		); err != nil {
			return err
		}
	}
	return nil
}

func upsertTags(tx *sql.Tx, bookID int64, tags []string) error {
	if _, err := tx.Exec(`DELETE FROM book_tags WHERE book_id=?`, bookID); err != nil {
		return err
	}
	for _, tag := range tags {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO tags (name) VALUES (?)`, tag); err != nil {
			return err
		}
		var tagID int64
		if err := tx.QueryRow(`SELECT id FROM tags WHERE name=?`, tag).Scan(&tagID); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO book_tags (book_id, tag_id) VALUES (?, ?)`, bookID, tagID); err != nil {
			return err
		}
	}
	return nil
}

// upsertSeries points the book at its series or clears series_id, then removes
// orphaned series rows. It must run after the books row exists.
func upsertSeries(tx *sql.Tx, b *model.Book) error {
	var seriesID, seriesIndex any
	if b.Series != nil {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO series (name) VALUES (?)`, b.Series.Name); err != nil {
			return err
		}
		var id int64
		if err := tx.QueryRow(`SELECT id FROM series WHERE name=?`, b.Series.Name).Scan(&id); err != nil {
			return err
		}
		seriesID, seriesIndex = id, b.Series.Index
	}

	if _, err := tx.Exec(
		`UPDATE books SET series_id=?, series_index=? WHERE id=?`,
		seriesID, seriesIndex, b.Meta.ID,
	); err != nil {
		return err
	}

	return nil
}

func deleteBook(tx *sql.Tx, id int64) error {
	// ON DELETE CASCADE handles book_authors, book_tags, identifiers.
	if _, err := tx.Exec(`DELETE FROM books WHERE id=?`, id); err != nil {
		return err
	}
	return cleanupOrphans(tx)
}

// cleanupOrphans removes authors, series, and tags that are no longer
// referenced by any book.
func cleanupOrphans(tx *sql.Tx) error {
	queries := []string{
		`DELETE FROM authors WHERE id NOT IN (SELECT author_id FROM book_authors)`,
		`DELETE FROM series  WHERE id NOT IN (SELECT series_id  FROM books WHERE series_id IS NOT NULL)`,
		`DELETE FROM tags    WHERE id NOT IN (SELECT tag_id     FROM book_tags)`,
	}
	for _, q := range queries {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	return nil
}
