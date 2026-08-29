package index

import (
	"database/sql"
	"fmt"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/library/internal/index/dbsqlc"
)

// NeedsReindex reports whether the index must be rebuilt — true when the
// schema version is stale or there are pending operations that may not have
// completed.
func (idx *Index) NeedsReindex() (bool, error) {
	v, err := idx.getSchemaVersion()
	if err != nil {
		return true, err
	}
	if v != schemaVersion {
		return true, nil
	}

	count, err := idx.queries.CountPendingOps(idx.ctx)
	if err != nil {
		return true, err
	}
	return count > 0, nil
}

// BookPath pairs a book with its on-disk file state. Rebuild takes the two
// together rather than as a book slice plus a lookup table so that indexing a
// book without its drift bookkeeping is not representable — see drift.PathInfo
// for why a zero value there is not benign.
type BookPath struct {
	Book *book.Book
	Info drift.PathInfo
}

// ensureSchema checks the schema version and recreates tables if needed.
func (idx *Index) ensureSchema() error {
	v, err := idx.getSchemaVersion()
	if err != nil {
		return err
	}
	if v != schemaVersion {
		if err := idx.dropAllTables(); err != nil {
			return fmt.Errorf("resetting index schema: %w", err)
		}
		if _, err := idx.db.ExecContext(idx.ctx, schema); err != nil {
			return fmt.Errorf("resetting index schema: %w", err)
		}
	}
	return nil
}

// rebuildTx executes the rebuild transaction: clears tables, inserts books and skipped entries,
// updates the ID sequence, and removes the pending op marker.
func (idx *Index) rebuildTx(books []BookPath, skipped map[string]drift.PathInfo, maxID int64) error {
	opID := newOpID()
	if err := idx.wq.InsertPendingOp(idx.ctx, opID); err != nil {
		return err
	}

	return idx.withTx(func(q *dbsqlc.Queries, tx *sql.Tx) error {
		for _, t := range []string{
			"book_authors", "book_tags", "identifiers",
			"books", "authors", "series", "tags", "skipped_books",
		} {
			if _, err := tx.ExecContext(idx.ctx, "DELETE FROM "+t); err != nil {
				return err
			}
		}

		for _, bt := range books {
			if err := idx.insertBook(q, bt.Book, bt.Info); err != nil {
				return err
			}
		}

		for path, info := range skipped {
			if err := q.InsertSkippedBook(idx.ctx, dbsqlc.InsertSkippedBookParams{
				EpubPath:  path,
				EpubSize:  info.Size,
				EpubMtime: toUnixNano(info.EpubMtime),
				MetaMtime: toUnixNano(info.MetaMtime),
				MetaSize:  info.MetaSize,
			}); err != nil {
				return err
			}
		}

		if maxID > 0 {
			if err := q.SetBookIDSequence(idx.ctx, maxID); err != nil {
				return err
			}
		}

		return q.DeleteAllPendingOps(idx.ctx)
	})
}

// Rebuild replaces the entire index with books, dropping and recreating tables
// if the schema is stale. maxID advances the id sequence past all reindexed ids.
// skipped maps the library path of each directory that could not be indexed to
// the file state it had; they are recorded rather than forgotten so AllPathInfo
// can report every path the rebuild accounted for.
func (idx *Index) Rebuild(books []BookPath, skipped map[string]drift.PathInfo, maxID int64) error {
	if err := idx.ensureSchema(); err != nil {
		return err
	}

	if err := idx.rebuildTx(books, skipped, maxID); err != nil {
		return err
	}

	if err := idx.setSchemaVersion(schemaVersion); err != nil {
		return err
	}

	// Build query planner statistics so JOIN-heavy queries (every listing,
	// search, browse view) don't make catastrophically bad plan choices.
	// Without this ANALYZE, a 4000-row query went from 0.05s → 5s in one
	// published report (jvns.ca). It's cheap and runs only on rebuild.
	if _, err := idx.db.ExecContext(idx.ctx, "ANALYZE"); err != nil {
		return fmt.Errorf("analyzing: %w", err)
	}
	return nil
}
