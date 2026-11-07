package index

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/fsutil"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index/dbsqlc"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// Index holds separate writer and reader connections, so a read doesn't contend
// for the single writer slot. On SMB, Open makes both the one writer connection.
type Index struct {
	db      *sql.DB         // writer, SetMaxOpenConns(1)
	wq      *dbsqlc.Queries // writer queries; NextID and pending_ops
	readDB  *sql.DB         // reader, up to 4 concurrent
	queries *dbsqlc.Queries // reader queries; everything read-only
	ctx     context.Context
}

const schemaVersion = 14

// dsn spells each pragma as key(value), which modernc.org/sqlite turns into
// "PRAGMA key=value" on every new connection the pool creates.
func dsn(path string, pragmas ...string) string {
	q := url.Values{}
	for _, p := range pragmas {
		q.Add("_pragma", p)
	}
	return path + "?" + q.Encode()
}

// mmapPragma turns memory mapping off on network filesystems. SQLite reports an
// I/O error on a mapped file as a signal, and SIGBUS kills the Go process where
// a read would have returned a query error.
func mmapPragma(fsType fsutil.FSType) string {
	if isNetwork(fsType) {
		return "mmap_size(0)"
	}
	return "mmap_size(134217728)"
}

// busyPragma waits longer on SMB, where a client that died can leave its
// EXCLUSIVE lock on the server until the server drops the client's handles.
func busyPragma(fsType fsutil.FSType) string {
	if fsType.IsSMB() {
		return "busy_timeout(60000)"
	}
	return "busy_timeout(5000)"
}

func isNetwork(fsType fsutil.FSType) bool {
	return fsType == fsutil.FSTypeNFS || fsType.IsSMB()
}

func writerPragmas(fsType fsutil.FSType) []string {
	pragmas := []string{
		busyPragma(fsType),
		"journal_size_limit(27103364)",
		mmapPragma(fsType),
		"cache_size(-8000)",
		"temp_store(memory)",
		"foreign_keys(ON)",
	}
	switch fsType {
	case fsutil.FSTypeNFS:
		pragmas = append([]string{"journal_mode(DELETE)", "synchronous(FULL)"}, pragmas...)
	case fsutil.FSTypeCIFS, fsutil.FSTypeSMB2:
		// WAL would map a -shm file on every open but the first, since modernc
		// runs journal_mode before locking_mode. DELETE never creates one.
		pragmas = append([]string{"journal_mode(DELETE)", "synchronous(FULL)", "locking_mode(EXCLUSIVE)"}, pragmas...)
	default: // local, FUSE, unknown
		pragmas = append([]string{"journal_mode(WAL)", "synchronous(NORMAL)"}, pragmas...)
	}
	return pragmas
}

func readerPragmas(fsType fsutil.FSType) []string {
	pragmas := []string{
		"busy_timeout(5000)",
		"query_only",
		mmapPragma(fsType),
		"cache_size(-8000)",
		"temp_store(memory)",
	}
	switch fsType {
	case fsutil.FSTypeNFS:
		pragmas = append([]string{"journal_mode(DELETE)"}, pragmas...)
	default: // local, FUSE, CIFS, SMB2, unknown
		pragmas = append([]string{"journal_mode(WAL)"}, pragmas...)
	}
	return pragmas
}

func Open(path string, allowSMB bool) (*Index, error) {
	fsType, err := fsutil.Check(filepath.Dir(path), allowSMB, "index")
	if err != nil {
		return nil, err
	}
	if fsType.IsSMB() {
		slog.Warn("a lock left by a dead SMB client can delay opening the index", "path", path)
	}

	db, err := sql.Open("sqlite", dsn(path, writerPragmas(fsType)...))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	var v int64
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		db.Close()
		return nil, err
	}

	// journal_mode keeps the old mode rather than failing when it can't switch.
	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		db.Close()
		return nil, err
	}
	if isNetwork(fsType) && journalMode != "delete" {
		slog.Warn("index stayed out of DELETE journal mode on a network filesystem", "path", path, "journal_mode", journalMode)
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

	// In EXCLUSIVE mode, the writer connection holds the exclusive lock,
	// so we can't have separate reader connections. Use the writer for reads.
	if fsType.IsSMB() {
		return &Index{
			db:      db,
			wq:      dbsqlc.New(db),
			readDB:  db,
			queries: dbsqlc.New(db),
			ctx:     context.Background(),
		}, nil
	}

	readDB, err := sql.Open("sqlite", dsn(path, readerPragmas(fsType)...))
	if err != nil {
		db.Close()
		return nil, err
	}
	readDB.SetMaxOpenConns(4)
	readDB.SetMaxIdleConns(4)

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
