package index

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"fmt"
	"net/url"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index/dbsqlc"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// Index holds two connections in WAL mode, so a read neither blocks another
// read nor contends for the single writer slot.
type Index struct {
	db      *sql.DB         // writer, SetMaxOpenConns(1)
	wq      *dbsqlc.Queries // writer queries; NextID and pending_ops
	readDB  *sql.DB         // reader, up to 4 concurrent
	queries *dbsqlc.Queries // reader queries; everything read-only
	ctx     context.Context
}

const schemaVersion = 13

// dsn spells each pragma as key(value), which modernc.org/sqlite turns into
// "PRAGMA key=value" on every new connection the pool creates.
func dsn(path string, pragmas ...string) string {
	q := url.Values{}
	for _, p := range pragmas {
		q.Add("_pragma", p)
	}
	return path + "?" + q.Encode()
}

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

func Open(path string) (*Index, error) {
	db, err := sql.Open("sqlite", dsn(path, writerPragmas()...))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

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
	// Updates the planner statistics, which improves long-term query plans.
	_, _ = idx.db.ExecContext(idx.ctx, "PRAGMA optimize")
	idx.readDB.Close()
	return idx.db.Close()
}

// NextID runs before Put, which needs the id to build the canonical path.
func (idx *Index) NextID() (int64, error) {
	return idx.wq.NextBookID(idx.ctx)
}

func newOpID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// withTx hands fn the raw transaction as well as the queries handle, for
// rebuildTx, whose table-by-table DELETE has no generated query.
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
