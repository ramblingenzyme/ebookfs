package kepub

import (
	"context"
	"errors"
	bookmodel "github.com/ramblingenzyme/ebookfs/internal/book"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

func TestCacheClose(t *testing.T) {
	c := NewCache(t.TempDir(), noopSource{})

	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

type noopSource struct{}

func (noopSource) Content(int64) (model.EpubReader, error) {
	return nil, errors.New("not used in this test")
}

func TestWarmerWarmsBook(t *testing.T) {
	var (
		mu   sync.Mutex
		seen []int64
	)
	w := newWarmer(func(b *bookmodel.Book) error {
		mu.Lock()
		seen = append(seen, b.Meta.ID)
		mu.Unlock()
		return nil
	})
	t.Cleanup(func() { close(w.ch) })

	w.warm(makeBook(1, "Test", "Author"))

	if !waitForWarm(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) == 1 && seen[0] == 1
	}) {
		t.Fatal("timed out waiting for warm")
	}
}

func TestWarmerWarmsMultipleBooks(t *testing.T) {
	var (
		mu   sync.Mutex
		seen []int64
	)
	w := newWarmer(func(b *bookmodel.Book) error {
		mu.Lock()
		seen = append(seen, b.Meta.ID)
		mu.Unlock()
		return nil
	})
	t.Cleanup(func() { close(w.ch) })

	w.warm(makeBook(1, "A", "Author"))
	w.warm(makeBook(2, "B", "Author"))

	if !waitForWarm(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) == 2
	}) {
		t.Fatal("timed out waiting for both warms")
	}
}

func TestWarmerErrorDoesNotPanic(t *testing.T) {
	done := make(chan struct{})
	w := newWarmer(func(b *bookmodel.Book) error {
		defer close(done)
		return testutil.ErrTest
	})
	t.Cleanup(func() { close(w.ch) })

	w.warm(makeBook(1, "Test", "Author"))

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("warmer goroutine did not call ensure function")
	}
}

// warm after stop must drop the hint, not send on the closed channel (which
// would panic). This is the shutdown-window race: the 9P server can call Warm
// while Cache.Close is tearing the warmer down.
func TestWarmAfterStopNoPanic(t *testing.T) {
	w := newWarmer(func(*bookmodel.Book) error { return nil })
	w.stop()
	w.warm(makeBook(1, "Test", "Author"))
}

// Warm called concurrently with stop must never panic. Run under -race.
func TestWarmConcurrentWithStopNoPanic(t *testing.T) {
	w := newWarmer(func(*bookmodel.Book) error { return nil })

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			b := makeBook(1, "Test", "Author")
			for range 1000 {
				w.warm(b)
			}
		})
	}

	w.stop()
	wg.Wait()
}

func TestCacheFilename(t *testing.T) {
	c := NewCache(t.TempDir(), noopSource{})

	tests := []struct {
		name     string
		epubName string
		want     string
	}{
		{"basic", "Book.epub", "Book.kepub.epub"},
		{"multiple dots", "My.Book.v2.epub", "My.Book.v2.kepub.epub"},
		{"no suffix", "Book", "Book.kepub.epub"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := makeBook(1, "Test", "Alice")
			b.EpubPath = tt.epubName
			got := c.Filename(b)
			if got != tt.want {
				t.Errorf("Filename = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCacheSize(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, noopSource{})
	b := makeBook(1, "Test", "Alice")

	// No cache file yet — should report cold.
	_, ok := c.Size(b)
	if ok {
		t.Error("Size should report cold for missing cache file")
	}

	// Create a cache file.
	cachePath := filepath.Join(dir, "1.kepub.epub")
	if err := os.WriteFile(cachePath, []byte("kepub-data"), 0644); err != nil {
		t.Fatal(err)
	}

	size, ok := c.Size(b)
	if !ok {
		t.Fatal("Size should report hot after cache file created")
	}
	if size != 10 { // len("kepub-data")
		t.Errorf("Size = %d, want 10", size)
	}
}

// fakeSource returns a temp file filled with the given data.
type fakeSource struct {
	t   *testing.T
	dir string
}

// srcContent wraps an *os.File to satisfy model.EpubReader. The kepub cache
// only reads from the reader; OPF and Cover are never called.
type srcContent struct {
	*os.File
}

func (c *srcContent) OPF() ([]byte, error)   { return nil, nil }
func (c *srcContent) Cover() ([]byte, error) { return nil, nil }

func (s fakeSource) Content(_ int64) (model.EpubReader, error) {
	path := filepath.Join(s.dir, "source.epub")
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &srcContent{f}, nil
}

func TestCacheEnsureCreatesFile(t *testing.T) {
	dir := t.TempDir()
	src := fakeSource{t: t, dir: dir}
	c := NewCache(dir, src)
	c.convertFn = func(_ context.Context, w io.Writer, _ io.ReaderAt, _ int64) error {
		_, err := w.Write([]byte("fake-kepub"))
		return err
	}

	srcPath := filepath.Join(dir, "source.epub")
	if err := os.WriteFile(srcPath, []byte("epub-data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Set DateModified in the future so the cache will be considered stale.
	b := makeBook(1, "Test", "Alice")
	b.Meta.DateModified = time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	b.EpubSize = int64(len("epub-data"))

	if err := c.Ensure(b); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	cachePath := filepath.Join(dir, "1.kepub.epub")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("cache file not created: %v", err)
	}
	if string(data) != "fake-kepub" {
		t.Errorf("cache content = %q, want %q", string(data), "fake-kepub")
	}
}

func TestCacheEnsureFreshIsNoop(t *testing.T) {
	dir := t.TempDir()
	src := fakeSource{t: t, dir: dir}
	c := NewCache(dir, src)
	var convertCalls int
	c.convertFn = func(_ context.Context, w io.Writer, _ io.ReaderAt, _ int64) error {
		convertCalls++
		_, err := w.Write([]byte("modified"))
		return err
	}

	srcPath := filepath.Join(dir, "source.epub")
	if err := os.WriteFile(srcPath, []byte("epub-data"), 0644); err != nil {
		t.Fatal(err)
	}

	cachePath := filepath.Join(dir, "1.kepub.epub")
	if err := os.WriteFile(cachePath, []byte("fresh-cache"), 0644); err != nil {
		t.Fatal(err)
	}

	// Set DateModified in the past so the cache (just created) is clearly fresher.
	b := makeBook(1, "Test", "Alice")
	b.Meta.DateModified = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	b.EpubSize = 9

	if err := c.Ensure(b); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if convertCalls != 0 {
		t.Error("Ensure should not convert when cache is fresh")
	}
	data, _ := os.ReadFile(cachePath)
	if string(data) != "fresh-cache" {
		t.Errorf("cache content changed to %q, want %q", string(data), "fresh-cache")
	}
}

func TestCacheEnsureWithZeroDateModified(t *testing.T) {
	dir := t.TempDir()
	src := fakeSource{t: t, dir: dir}
	c := NewCache(dir, src)
	c.convertFn = func(_ context.Context, w io.Writer, _ io.ReaderAt, _ int64) error {
		_, err := w.Write([]byte("kepub-content"))
		return err
	}

	srcPath := filepath.Join(dir, "source.epub")
	if err := os.WriteFile(srcPath, []byte("epub-data"), 0644); err != nil {
		t.Fatal(err)
	}

	b := makeBook(1, "Test", "Alice")
	b.EpubSize = 9

	// Ensure the cache file is created, then verify it on disk.
	if err := c.Ensure(b); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	data, err := os.ReadFile(c.path(b))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "kepub-content" {
		t.Errorf("cache content = %q, want %q", string(data), "kepub-content")
	}
}

func TestCacheWarmProducesFile(t *testing.T) {
	dir := t.TempDir()
	src := fakeSource{t: t, dir: dir}
	c := NewCache(dir, src)
	c.convertFn = func(_ context.Context, w io.Writer, _ io.ReaderAt, _ int64) error {
		_, err := w.Write([]byte("warm-content"))
		return err
	}

	srcPath := filepath.Join(dir, "source.epub")
	if err := os.WriteFile(srcPath, []byte("epub-data"), 0644); err != nil {
		t.Fatal(err)
	}

	b := makeBook(1, "Warm", "Author")
	b.EpubSize = 9

	c.Warm(b)
	c.Close()

	cachePath := filepath.Join(dir, "1.kepub.epub")
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("cache file not created after warm: %v", err)
	}
}

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

var makeBook = bookmodel.MakeMutableBook
