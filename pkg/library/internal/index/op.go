package index

import (
	"database/sql"
	"errors"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index/dbsqlc"
)

// Op is one mutation's pending row. The caller gets one from MarkPending
// before touching disk, then Put or Delete commits the index write and clears
// the row in one transaction.
type Op struct {
	idx  *Index
	opID string
}

// MarkPending writes its row by autocommit, so it survives a crash. A row left
// behind by a failed operation forces a healing reindex on the next startup.
func (idx *Index) MarkPending() (*Op, error) {
	id := newOpID()
	if err := idx.wq.InsertPendingOp(idx.ctx, id); err != nil {
		return nil, err
	}
	return &Op{idx: idx, opID: id}, nil
}

func (o *Op) finish(fn func(*dbsqlc.Queries) error) error {
	if o.opID == "" {
		return errors.New("op was cancelled")
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
