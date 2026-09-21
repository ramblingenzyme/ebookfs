package kepub

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
)

// The hint reaches a conversion at all.
func TestCacheWarmProducesFile(t *testing.T) {
	c, dir := newTestCache(t, "warm-content")

	b := makeBook(1, "Warm", "Author")
	b.EpubSize = 9

	c.Warm(b)

	cachePath := filepath.Join(dir, "1.kepub.epub")
	if !waitForWarm(t, func() bool {
		_, err := os.Stat(cachePath)
		return err == nil
	}) {
		t.Error("cache file not created after warm")
	}
}

// Driven through the cache, since the warmer's only conversion path is Ensure.
func TestWarmerWarmsMultipleBooks(t *testing.T) {
	c, dir := newTestCache(t, "warm-content")

	a := makeBook(1, "A", "Author")
	a.EpubSize = 9
	b := makeBook(2, "B", "Author")
	b.EpubSize = 9
	c.Warm(a)
	c.Warm(b)

	if !waitForWarm(t, func() bool {
		_, err1 := os.Stat(filepath.Join(dir, "1.kepub.epub"))
		_, err2 := os.Stat(filepath.Join(dir, "2.kepub.epub"))
		return err1 == nil && err2 == nil
	}) {
		t.Fatal("timed out waiting for both warms")
	}
}

func TestWarmerErrorDoesNotPanic(t *testing.T) {
	c, _ := newTestCache(t, "unused")
	done := make(chan struct{})
	c.convertFn = func(context.Context, io.Writer, io.ReaderAt, int64) error {
		defer close(done)
		return util.ErrTest
	}

	b := makeBook(1, "Test", "Author")
	b.EpubSize = 9
	c.Warm(b)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("warmer goroutine did not convert")
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
	c.Warm(b)

	// A negative with nothing to wait on, so give the stopped pool a window.
	time.Sleep(50 * time.Millisecond)
	if converted.Load() {
		t.Error("Warm after Close reached the converter")
	}
	if _, err := os.Stat(filepath.Join(dir, "1.kepub.epub")); err == nil {
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
				c.Warm(b)
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
