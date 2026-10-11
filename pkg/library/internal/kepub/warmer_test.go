package kepub

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
)

// Driven through the cache, since the warmer's only conversion path is Ensure.
// The first warmerGoroutines conversions fail, so the books after them convert
// only if a goroutine carries on past its failure.
func TestWarmerWarmsMultipleBooks(t *testing.T) {
	c, dir := newTestCache(t, "warm-content")
	convert := c.convertFn
	var calls atomic.Int32
	c.convertFn = func(ctx context.Context, w io.Writer, r io.ReaderAt, n int64) error {
		if calls.Add(1) <= warmerGoroutines {
			return util.ErrTest
		}
		return convert(ctx, w, r, n)
	}

	books := warmerGoroutines + 2
	for id := int64(1); id <= int64(books); id++ {
		b := makeBook(id, "Book", "Author")
		b.EpubSize = 9
		c.Warm(wrapBook(b))
	}

	if !waitForWarm(t, func() bool {
		warmed := 0
		for id := 1; id <= books; id++ {
			if _, err := os.Stat(filepath.Join(dir, strconv.Itoa(id), ".sidecar", "kepub.epub")); err == nil {
				warmed++
			}
		}
		return warmed == 2
	}) {
		t.Fatal("timed out waiting for the two books after the failures to warm")
	}
}

// The 9P server can call Warm while the cache is being torn down.
func TestWarmAfterCloseDoesNotConvert(t *testing.T) {
	c, dir := newTestCache(t, "warm-content")
	var converted atomic.Bool
	c.convertFn = func(context.Context, io.Writer, io.ReaderAt, int64) error {
		converted.Store(true)
		return nil
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	b := makeBook(1, "Test", "Author")
	b.EpubSize = 9
	c.Warm(wrapBook(b))

	// A negative with nothing to wait on, so give the stopped pool a window.
	time.Sleep(50 * time.Millisecond)
	if converted.Load() {
		t.Error("Warm after Close reached the converter")
	}
	cachePath := filepath.Join(dir, "1", ".sidecar", "kepub.epub")
	if _, err := os.Stat(cachePath); err == nil {
		t.Error("Warm after Close produced a cache file")
	}
}

// The regression guard on warmer.ch never being closed. Run under -race.
func TestWarmConcurrentWithCloseNoPanic(t *testing.T) {
	c, _ := newTestCache(t, "warm-content")

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			b := makeBook(1, "Test", "Author")
			for range 1000 {
				c.Warm(wrapBook(b))
			}
		})
	}

	c.Close()
	wg.Wait()
}

// waitForWarm polls because a warm completes on a goroutine with nothing to join.
func waitForWarm(t *testing.T, f func() bool) bool {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if f() {
			return true
		}
		select {
		case <-deadline:
			return false
		case <-time.After(10 * time.Millisecond):
		}
	}
}
