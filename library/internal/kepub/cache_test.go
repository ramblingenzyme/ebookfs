package kepub

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
)

// noopSource is for tests that never reach a conversion.
type noopSource struct{}

func (noopSource) Content(int64) (epub.EpubReader, error) {
	return nil, errors.New("not used in this test")
}

func TestCacheClose(t *testing.T) {
	c := NewCache(t.TempDir(), noopSource{})

	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// Close must not wait out a conversion in flight: it is called after the 9P
// server is down, past main's shutdown deadline. Warm is only how this test
// gets a conversion running.
func TestCacheCloseCancelsConversion(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, fakeSource{t: t, dir: dir})
	started := make(chan struct{})
	c.convertFn = func(ctx context.Context, w io.Writer, _ io.ReaderAt, _ int64) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}

	if err := os.WriteFile(filepath.Join(dir, "source.epub"), []byte("epub-data"), 0644); err != nil {
		t.Fatal(err)
	}
	b := makeBook(1, "Warm", "Author")
	b.EpubSize = 9

	c.Warm(b)
	<-started

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Close()
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close blocked on the conversion in flight")
	}
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

func TestCacheEnsureCreatesFile(t *testing.T) {
	c, dir := newTestCache(t, "fake-kepub")

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
	c, _ := newTestCache(t, "kepub-content")

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
