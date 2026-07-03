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

const schemaVersion = 5

// Open opens or creates the index at path.
func Open(path string) (*Index, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)

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
		// Fresh database: apply the current schema and stamp the version.
		if _, err := db.Exec(schema); err != nil {
			db.Close()
			return nil, fmt.Errorf("applying schema: %w", err)
		}
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version=%d", schemaVersion)); err != nil {
			db.Close()
			return nil, err
		}
	}

	return &Index{db: db}, nil
}

func (idx *Index) dropAllTables() error {
	rows, err := idx.db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, t := range tables {
		if _, err := idx.db.Exec(`DROP TABLE IF EXISTS ` + t); err != nil {
			return err
		}
	}
	return nil
}

func (idx *Index) Close() error {
	return idx.db.Close()
}

// NextID reserves and returns a new unique book ID. Must be called before
// Put so the id is available for canonical path construction.
func (idx *Index) NextID() (int64, error) {
	var id int64
	err := idx.db.QueryRow("INSERT INTO book_id_seq DEFAULT VALUES RETURNING id").Scan(&id)
	return id, err
}

// setDirty marks the index as potentially inconsistent, forcing a reindex
// on the next startup if a crash occurs before withTx clears the flag.
func (idx *Index) setDirty() error {
	_, err := idx.db.Exec("UPDATE library_meta SET dirty = 1")
	return err
}

// withTx sets the dirty flag, runs fn in a transaction, then clears the flag
// on commit. setDirty runs via idx.db (outside the tx), so a crash before
// tx.Commit leaves the flag set and forces a reindex.
func (idx *Index) withTx(fn func(*sql.Tx) error) error {
	if err := idx.setDirty(); err != nil {
		return err
	}
	tx, err := idx.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec("UPDATE library_meta SET dirty = 0"); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
