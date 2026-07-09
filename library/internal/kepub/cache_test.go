package kepub

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

func TestCacheClose(t *testing.T) {
	c := NewCache(t.TempDir(), noopSource{})
	c.Close()
}

type noopSource struct{}

func (noopSource) OpenEpub(int64) (model.EpubReader, error) {
	return nil, errors.New("not used in this test")
}

func TestWarmerWarmsBook(t *testing.T) {
	var (
		mu   sync.Mutex
		seen []int64
	)
	w := newWarmer(func(b *model.Book) error {
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
	w := newWarmer(func(b *model.Book) error {
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
	w := newWarmer(func(b *model.Book) error {
		defer close(done)
		return errTest
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
	w := newWarmer(func(*model.Book) error { return nil })
	w.stop()
	w.warm(makeBook(1, "Test", "Author")) // must not panic
}

// Warm called concurrently with stop must never panic. Run under -race.
func TestWarmConcurrentWithStopNoPanic(t *testing.T) {
	w := newWarmer(func(*model.Book) error { return nil })

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b := makeBook(1, "Test", "Author")
			for j := 0; j < 1000; j++ {
				w.warm(b)
			}
		}()
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
			b.EpubFilename = tt.epubName
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

var errTest = errors.New("test error")

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

func makeBook(id int64, title string, authors ...string) *model.Book {
	auths := make([]model.Author, len(authors))
	for i, name := range authors {
		auths[i] = model.Author{Name: name}
	}
	return model.NewBook(
		model.Bib{Title: title, Authors: auths},
		model.Meta{ID: id},
		model.Location{},
	)
}
