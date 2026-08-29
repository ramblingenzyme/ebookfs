package index

import (
	"database/sql"
	"errors"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/library/internal/index/dbsqlc"
)

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
	if err := o.idx.wq.InsertPendingOp(o.idx.ctx, id); err != nil {
		return err
	}
	o.opID = id
	return nil
}

// finish runs fn and clears the pending row in one transaction. MarkPending
// must have been called first so a pending row protects the preceding store
// writes; the row is atomically deleted inside the same transaction.
func (o *Op) finish(fn func(*dbsqlc.Queries, *sql.Tx) error) error {
	if o.opID == "" {
		return errors.New("MarkPending must be called before commit")
	}
	return o.idx.withTx(func(q *dbsqlc.Queries, tx *sql.Tx) error {
		if err := fn(q, tx); err != nil {
			return err
		}
		return q.DeletePendingOp(o.idx.ctx, o.opID)
	})
}

// Cancel deletes the pending-op row without touching store data. Call it when
// an operation fails after MarkPending, so the next startup skips the reindex.
func (o *Op) Cancel() {
	if o.opID == "" {
		return
	}

	if err := o.idx.wq.DeletePendingOp(o.idx.ctx, o.opID); err == nil {
		o.opID = ""
	}
}

// Put writes b into the index, inserting or replacing the record for b.Meta.ID.
// mt carries the on-disk file state used for drift detection.
func (o *Op) Put(b *book.Book, mt drift.PathInfo) error {
	return o.finish(func(q *dbsqlc.Queries, tx *sql.Tx) error { return o.idx.putBook(q, b, mt) })
}

// Delete removes all index rows for book.
func (o *Op) Delete(bookID int64) error {
	return o.finish(func(q *dbsqlc.Queries, tx *sql.Tx) error { return o.idx.deleteBook(q, bookID) })
}
