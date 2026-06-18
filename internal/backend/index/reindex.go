package index

import (
	"database/sql"
	"fmt"

	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

// Rebuild replaces the entire derived index with books. If the schema version
// is out of date, all tables are dropped and recreated before reinserting —
// tying the schema reset to the reindex so the two are never decoupled.
// maxID is the highest book id present on disk; the id sequence is advanced
// past it so future NextID values cannot collide with reindexed ids.
//
// TODO: invert this coupling. If startup reindexing becomes conditional (e.g.
// skipped when the book count matches), the schema version check should move
// up to the library or server layer so an outdated schema can force a reindex
// rather than being silently skipped.
func (idx *Index) Rebuild(books []*model.Book, maxID int64) error {
	var v int64
	if err := idx.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	if v != schemaVersion {
		if err := idx.dropAllTables(); err != nil {
			return fmt.Errorf("resetting index schema: %w", err)
		}
		if _, err := idx.db.Exec(schema); err != nil {
			return fmt.Errorf("resetting index schema: %w", err)
		}
	}

	if err := idx.withTx(func(tx *sql.Tx) error {
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
			if err := putBook(tx, b); err != nil {
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
	}); err != nil {
		return err
	}

	_, err := idx.db.Exec(fmt.Sprintf("PRAGMA user_version=%d", schemaVersion))
	return err
}
