package index

import "database/sql"

// Index is a SQLite-backed cache of the library. It is derived from the
// filesystem and can be fully rebuilt via Reindex.
type Index struct {
	db *sql.DB
}

// Open opens or creates the index at path, running any pending migrations.
func Open(path string) (*Index, error) {
	panic("not yet implemented")
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
	panic("not yet implemented")
}

