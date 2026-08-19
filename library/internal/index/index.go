package index

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"fmt"
	"net/url"

	"github.com/ramblingenzyme/ebookfs/library/internal/index/dbsqlc"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// Index is a SQLite-backed cache of the library. It is derived from the
// filesystem and can be fully rebuilt via Reindex.
//
// Two database connections are held in WAL mode: one writer (db) and one
// reader (readDB). Reads are routed through the reader connection so they
// never block each other or contend with the single writer slot.
type Index struct {
	db      *sql.DB         // writer connection (single writer with SetMaxOpenConns(1))
	wq      *dbsqlc.Queries // writer queries wrapping db (used by NextID, pending_ops)
	readDB  *sql.DB         // reader connection (up to 4 concurrent readers)
	queries *dbsqlc.Queries // reader queries wrapping readDB (used for all read-only queries)
	ctx     context.Context
}

const schemaVersion = 12

// dsn returns a sqlite DSN for path with the given per-connection PRAGMAs
// applied via _pragma query parameters.  Each pragma uses key(value) syntax
// which modernc.org/sqlite translates to "PRAGMA key=value" on every new
// connection the pool creates.
func dsn(path string, pragmas ...string) string {
	q := url.Values{}
	for _, p := range pragmas {
		q.Add("_pragma", p)
	}
	return path + "?" + q.Encode()
}

// writerPragmas are applied to every writer connection.
func writerPragmas() []string {
	return []string{
		"journal_mode(WAL)",
		// In WAL mode, NORMAL is crash-safe and avoids an extra fsync per write.
		"synchronous(NORMAL)",
		"busy_timeout(5000)",
		"journal_size_limit(27103364)",
		"mmap_size(134217728)",
		"cache_size(-8000)",
		"temp_store(memory)",
		"foreign_keys(ON)",
	}
}

// readerPragmas are applied to every reader connection.
func readerPragmas() []string {
	return []string{
		"journal_mode(WAL)",
		"busy_timeout(5000)",
		"query_only",
		"mmap_size(134217728)",
		"cache_size(-8000)",
		"temp_store(memory)",
	}
}

// Open opens or creates the index at path.
func Open(path string) (*Index, error) {
	db, err := sql.Open("sqlite", dsn(path, writerPragmas()...))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Open a second connection pool for reads.  WAL mode allows concurrent
	// readers without blocking writers, so read queries never contend with
	// each other or stall behind a write in progress.
	readDB, err := sql.Open("sqlite", dsn(path, readerPragmas()...))
	if err != nil {
		db.Close()
		return nil, err
	}
	readDB.SetMaxOpenConns(4)
	readDB.SetMaxIdleConns(4)

	var v int64
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		readDB.Close()
		db.Close()
		return nil, err
	}

	if v == 0 {
		// Fresh database: apply the schema but leave user_version at 0 so
		// NeedsReindex forces the first reindex. An empty pending_ops table is
		// the normal clean state and cannot distinguish a fresh index from a
		// completed one, so Rebuild is left as the sole version-stamper.
		if _, err := db.Exec(schema); err != nil {
			readDB.Close()
			db.Close()
			return nil, fmt.Errorf("applying schema: %w", err)
		}
	}

	return &Index{
		db:      db,
		wq:      dbsqlc.New(db),
		readDB:  readDB,
		queries: dbsqlc.New(readDB),
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
	// Let SQLite analyze schema usage and update planner statistics before
	// closing — an inexpensive operation that improves long-term query plans.
	_, _ = idx.db.ExecContext(idx.ctx, "PRAGMA optimize")
	idx.readDB.Close()
	return idx.db.Close()
}

// NextID reserves and returns a new unique book ID. Must be called before
// Put so the id is available for canonical path construction.
func (idx *Index) NextID() (int64, error) {
	return idx.wq.NextBookID(idx.ctx)
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
	err := idx.readDB.QueryRowContext(idx.ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}

func (idx *Index) setSchemaVersion(v int64) error {
	_, err := idx.db.ExecContext(idx.ctx, fmt.Sprintf("PRAGMA user_version=%d", v))
	return err
}
