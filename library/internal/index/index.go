package index

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"fmt"

	"github.com/ramblingenzyme/ebookfs/library/internal/index/dbsqlc"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// Index is a SQLite-backed cache of the library. It is derived from the
// filesystem and can be fully rebuilt via Reindex.
type Index struct {
	db      *sql.DB
	queries *dbsqlc.Queries
	ctx     context.Context
}

const schemaVersion = 9

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

	return &Index{
		db:      db,
		queries: dbsqlc.New(db),
		ctx:     context.Background(),
	}, nil
}

func (idx *Index) dropAllTables() error {
	rows, err := idx.db.QueryContext(idx.ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
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
	if _, err := idx.db.ExecContext(idx.ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	for _, t := range tables {
		if _, err := idx.db.ExecContext(idx.ctx, `DROP TABLE IF EXISTS `+t); err != nil {
			return err
		}
	}
	if _, err := idx.db.ExecContext(idx.ctx, `PRAGMA foreign_keys=ON`); err != nil {
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
	return idx.queries.NextBookID(idx.ctx)
}

// newOpID returns a random hex string used as a unique pending-op identifier.
func newOpID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// withTx runs fn inside a SQLite transaction, committing on success and
// rolling back on error.
func (idx *Index) withTx(fn func(*dbsqlc.Queries, *sql.Tx) error) error {
	tx, err := idx.db.Begin()
	if err != nil {
		return err
	}
	q := dbsqlc.New(tx)
	if err := fn(q, tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (idx *Index) getSchemaVersion() (int64, error) {
	var v int64
	err := idx.db.QueryRowContext(idx.ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}

func (idx *Index) setSchemaVersion(v int64) error {
	_, err := idx.db.ExecContext(idx.ctx, fmt.Sprintf("PRAGMA user_version=%d", v))
	return err
}
