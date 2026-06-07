package index

import (
	"database/sql"

	"github.com/ramblingenzyme/ebookfs/internal/model"
)

// Rebuild replaces the entire derived index with books, in a single transaction
// so that a failure leaves the previous index intact. maxID is the highest book
// id present on disk; the id sequence is advanced past it so future NextID
// values cannot collide with reindexed ids (e.g. after the index db was lost).
func (idx *Index) Rebuild(books []*model.Book, maxID int64) error {
	return idx.withTx(func(tx *sql.Tx) error {
		// Children before parents so the deletes are valid with foreign keys on.
		for _, t := range []string{
			"book_authors", "book_tags", "identifiers",
			"books", "authors", "series", "tags",
		} {
			if _, err := tx.Exec("DELETE FROM " + t); err != nil {
				return err
			}
		}

		for _, b := range books {
			if err := insertBook(tx, b); err != nil {
				return err
			}
		}

		// Inserting an explicit id bumps the AUTOINCREMENT high-water mark; the
		// ON CONFLICT keeps it a no-op when the sequence is already ahead.
		if maxID > 0 {
			if _, err := tx.Exec(
				`INSERT INTO book_id_seq(id) VALUES(?) ON CONFLICT(id) DO NOTHING`,
				maxID,
			); err != nil {
				return err
			}
		}
		return nil
	})
}
