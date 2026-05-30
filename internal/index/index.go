package index

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// Index is a SQLite-backed cache of the library. It is derived from the
// filesystem and can be fully rebuilt via Reindex.
type Index struct {
	db *sql.DB
}

// Open opens or creates the index at path, running any pending migrations.
func Open(path string) (*Index, error) {
	// - Register the modernc.org/sqlite driver and call sql.Open("sqlite", path).
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, err
	}

	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return nil, err
	}

	var v int64
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return nil, err
	}

	if v == 0 {
		if _, err := db.Exec(schema); err != nil {
			db.Close()
			return nil, fmt.Errorf("applying schema: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version=1"); err != nil {
			db.Close()
			return nil, err
		}
	}

	if _, err := db.Exec(
		`CREATE TABLE IF NOT EXISTS book_id_seq (id INTEGER PRIMARY KEY AUTOINCREMENT)`,
	); err != nil {
		db.Close()
		return nil, err
	}

	return &Index{db: db}, nil
}

func (idx *Index) Close() error {
	return idx.db.Close()
}

// Begin starts a new transaction. The caller is responsible for Commit or Rollback.
func (idx *Index) Begin() (*sql.Tx, error) {
	return idx.db.Begin()
}

// AllocateID reserves and returns a new unique book ID within tx.
// Must be called before InsertBook so the id is available for canonical path construction.
func (idx *Index) AllocateID(tx *sql.Tx) (int64, error) {
	if _, err := tx.Exec("INSERT INTO book_id_seq DEFAULT VALUES"); err != nil {
		return 0, err
	}
	var id int64
	if err := tx.QueryRow("SELECT last_insert_rowid()").Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}
