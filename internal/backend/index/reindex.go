package index

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

// NeedsReindex reports whether the index must be rebuilt — true when the
// schema version is stale or the dirty flag is set.
func (idx *Index) NeedsReindex() (bool, error) {
	var v int64
	if err := idx.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return true, err
	}
	if v != schemaVersion {
		return true, nil
	}
	var dirty int64
	err := idx.db.QueryRow("SELECT dirty FROM library_meta LIMIT 1").Scan(&dirty)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil // no meta row — fresh or corrupt database
	}
	if err != nil {
		return true, err
	}
	return dirty != 0, nil
}

// Rebuild replaces the entire index with books, dropping and recreating tables
// if the schema is stale. maxID advances the id sequence past all reindexed ids.
// The dirty flag is cleared inside the same transaction as the book data.
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
			"library_meta",
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

		// Insert the meta row as clean; withTx's UPDATE dirty=0 is redundant.
		if _, err := tx.Exec("INSERT INTO library_meta (dirty) VALUES (0)"); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	_, err := idx.db.Exec(fmt.Sprintf("PRAGMA user_version=%d", schemaVersion))
	return err
}
