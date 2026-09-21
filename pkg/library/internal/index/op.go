package index

import (
	"database/sql"
	"errors"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index/dbsqlc"
)

// Op represents a single mutation operation. The caller calls BeginOp to
// obtain one, optionally calls MarkPending before touching disk, performs the
// store writes, then calls Op.Put or Op.Delete to commit the index write and
// atomically clear the pending row.
type Op struct {
	idx  *Index
	opID string
}

func (idx *Index) BeginOp() *Op {
	return &Op{idx: idx}
}

// MarkPending writes its row by autocommit, so it survives a crash, and is
// idempotent. A row left behind by a failed operation forces a healing reindex
// on the next startup.
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

func (o *Op) finish(fn func(*dbsqlc.Queries) error) error {
	if o.opID == "" {
		return errors.New("MarkPending must be called before commit")
	}
	return o.idx.withTx(func(q *dbsqlc.Queries, _ *sql.Tx) error {
		if err := fn(q); err != nil {
			return err
		}
		return q.DeletePendingOp(o.idx.ctx, o.opID)
	})
}

// Cancel is for an operation that failed after MarkPending, so the next
// startup skips the reindex.
func (o *Op) Cancel() {
	if o.opID == "" {
		return
	}

	if err := o.idx.wq.DeletePendingOp(o.idx.ctx, o.opID); err == nil {
		o.opID = ""
	}
}

// Put records mt as the file state drift detection compares against.
func (o *Op) Put(b *book.Book, mt drift.PathInfo) error {
	return o.finish(func(q *dbsqlc.Queries) error { return o.idx.putBook(q, b, mt) })
}

func (o *Op) Delete(bookID int64) error {
	return o.finish(func(q *dbsqlc.Queries) error { return o.idx.deleteBook(q, bookID) })
}
