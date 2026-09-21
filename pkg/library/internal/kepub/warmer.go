package kepub

import (
	"context"
	"log/slog"
	"sync"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

const (
	warmerGoroutines = 4
	warmerQueueSize  = 4096
)

// warmer converts kepubs off the read path, so a cache is built before the
// next rsync. Enqueue is non-blocking, and a full queue drops the warm, which
// costs nothing because the read path converts on demand.
type warmer struct {
	// ensure is Cache.Ensure, held as a function rather than the cache itself,
	// so this file's only coupling to the cache is visible here.
	ensure func(*book.Book) error

	// Never closed. The ctx passed to run is the only stop signal, which lets
	// warm send from any goroutine without coordinating with shutdown. Closing
	// would panic those senders and buy only a drained backlog, which Close
	// does not want.
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
				// Cancellation arrives as whatever kepubify returned, so the ctx
				// is what distinguishes it from a real failure. Without this the
				// backlog logs one warning per queued book on every shutdown.
				if ctx.Err() != nil {
					return
				}
				slog.Warn("kepub: warm book failed", "book_id", b.Meta.ID, "error", err)
			}
		}
	}
}

// Cancelling the ctx newWarmer was given is what makes the goroutines return.
func (w *warmer) wait() { w.wg.Wait() }
