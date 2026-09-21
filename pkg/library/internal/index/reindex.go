package index

import (
	"database/sql"
	"fmt"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index/dbsqlc"
)

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

// BookPath keeps the two together rather than letting Rebuild take a book
// slice plus a lookup table, so indexing a book without its drift bookkeeping
// is not representable. drift.PathInfo says what a zero value costs.
type BookPath struct {
	Book *book.Book
	Info drift.PathInfo
}

// ensureSchema discards the index, which is safe because it is derived from
// the filesystem (DECISIONS.md #2) and the caller rebuilds it.
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

// Rebuild records each directory it could not index in skipped, rather than
// forgetting it, so AllPathInfo can report every path the rebuild accounted
// for. maxID advances the id sequence past every reindexed id.
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

	// Every listing, search and browse view is JOIN-heavy, and without these
	// planner statistics one published report (jvns.ca) had a 4000-row query
	// go from 0.05s to 5s.
	if _, err := idx.db.ExecContext(idx.ctx, "ANALYZE"); err != nil {
		return fmt.Errorf("analyzing: %w", err)
	}
	return nil
}
