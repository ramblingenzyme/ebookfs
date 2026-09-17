package kepub

import (
	"context"
	"log/slog"
	"sync"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

const (
	warmerGoroutines = 4    // concurrent conversions
	warmerQueueSize  = 4096 // max backlog before drops
)

// warmer converts kepubs off the read path. The exporter's Warm method enqueues
// books here so their caches are built before the next rsync. Enqueue is
// non-blocking; a full queue drops the warm and the read path converts on demand.
type warmer struct {
	// ensure is Cache.Ensure, held as a function rather than the cache itself:
	// the one call this file makes into it is the whole coupling, and stating
	// it here keeps the queue readable without the cache open alongside.
	ensure func(*book.Book) error

	// Never closed: the ctx passed to run is the only stop signal, which is
	// what lets warm send from any goroutine at any time without coordinating
	// with shutdown. Closing would panic those senders, and buys only the
	// drain-the-backlog semantics Close deliberately does not want. Nothing is
	// leaked by leaving it open; a channel is collected like any other value.
	ch chan *book.Book
	wg sync.WaitGroup
}

func newWarmer(ctx context.Context, ensure func(*book.Book) error) *warmer {
	w := &warmer{ensure: ensure, ch: make(chan *book.Book, warmerQueueSize)}
	for range warmerGoroutines {
		w.wg.Go(func() { w.run(ctx) })
	}
	return w
}

func (w *warmer) warm(b *book.Book) {
	select {
	case w.ch <- b:
	default: // full, or nobody left to receive; drop the hint
	}
}

func (w *warmer) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case b := <-w.ch:
			if err := w.ensure(b); err != nil {
				// Read from the ctx rather than the error: cancellation reaches
				// here as whatever kepubify returned, and the backlog is
				// abandoned rather than logged a failure per queued book.
				if ctx.Err() != nil {
					return
				}
				slog.Warn("kepub: warm book failed", "book_id", b.Meta.ID, "error", err)
			}
		}
	}
}

// wait blocks until every warmer goroutine has returned. Cancelling the ctx
// newWarmer was given is what makes them return.
func (w *warmer) wait() { w.wg.Wait() }
