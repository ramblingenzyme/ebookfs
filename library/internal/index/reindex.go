package index

import (
	"database/sql"
	"fmt"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// NeedsReindex reports whether the index must be rebuilt — true when the
// schema version is stale or there are pending operations that may not have
// completed.
func (idx *Index) NeedsReindex() (bool, error) {
	var v int64
	if err := idx.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return true, err
	}
	if v != schemaVersion {
		return true, nil
	}
	var count int64
	if err := idx.db.QueryRow("SELECT COUNT(*) FROM pending_ops").Scan(&count); err != nil {
		return true, err
	}
	return count > 0, nil
}

// Rebuild replaces the entire index with books, dropping and recreating tables
// if the schema is stale. maxID advances the id sequence past all reindexed ids.
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

	opID := newOpID()
	if _, err := idx.db.Exec("INSERT INTO pending_ops (op_id) VALUES (?)", opID); err != nil {
		return err
	}

	if err := idx.withTx(func(tx *sql.Tx) error {
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

		if maxID > 0 {
			if _, err := tx.Exec(
				`INSERT INTO book_id_seq(id) VALUES(?) ON CONFLICT(id) DO NOTHING`,
				maxID,
			); err != nil {
				return err
			}
		}

		_, err := tx.Exec("DELETE FROM pending_ops")
		return err
	}); err != nil {
		return err
	}

	_, err := idx.db.Exec(fmt.Sprintf("PRAGMA user_version=%d", schemaVersion))
	return err
}
