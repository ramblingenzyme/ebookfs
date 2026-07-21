package index

import (
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
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

const schemaVersion = 8

// Open opens or creates the index at path.
func Open(path string) (*Index, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}

	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, err
	}

	var v int64
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		db.Close()
		return nil, err
	}

	if v == 0 {
		// Fresh database: apply the schema but leave user_version at 0 so
		// NeedsReindex forces the first reindex. An empty pending_ops table is
		// the normal clean state and cannot distinguish a fresh index from a
		// completed one, so Rebuild is left as the sole version-stamper.
		if _, err := db.Exec(schema); err != nil {
			db.Close()
			return nil, fmt.Errorf("applying schema: %w", err)
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
	if _, err := idx.db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	for _, t := range tables {
		if _, err := idx.db.Exec(`DROP TABLE IF EXISTS ` + t); err != nil {
			return err
		}
	}
	if _, err := idx.db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		return err
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

// newOpID returns a random hex string used as a unique pending-op identifier.
func newOpID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// withTx runs fn inside a SQLite transaction, committing on success and
// rolling back on error.
func (idx *Index) withTx(fn func(*sql.Tx) error) error {
	tx, err := idx.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Op represents a single mutation operation. The caller calls BeginOp to
// obtain one, optionally calls MarkPending before touching disk, performs the
// store writes, then calls Op.Put or Op.Delete to commit the index write and
// atomically clear the pending row.
type Op struct {
	idx  *Index
	opID string
}

// BeginOp starts a new mutation operation.
func (idx *Index) BeginOp() *Op {
	return &Op{idx: idx}
}

// MarkPending inserts a row into pending_ops via autocommit (so it survives a
// crash) and is idempotent — at most one row per operation. Call it before
// the first real disk mutation. If the operation fails after MarkPending the
// row stays behind, forcing a healing reindex on the next startup.
func (o *Op) MarkPending() error {
	if o.opID != "" {
		return nil
	}
	id := newOpID()
	if _, err := o.idx.db.Exec("INSERT INTO pending_ops (op_id) VALUES (?)", id); err != nil {
		return err
	}
	o.opID = id
	return nil
}
