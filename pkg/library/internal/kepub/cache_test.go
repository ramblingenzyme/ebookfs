package kepub

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Close must return with a conversion still in flight. Warm is only how this
// test gets one running.
func TestCacheCloseCancelsConversion(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(&fakeHost{dir: dir})
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

	c.Warm(wrapBook(b))
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
	c := NewCache(&fakeHost{dir: t.TempDir()})

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
			got := c.Filename(wrapBook(b))
			if got != tt.want {
				t.Errorf("Filename = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCacheSize(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(&fakeHost{dir: dir})
	b := makeBook(1, "Test", "Alice")

	_, ok := c.Size(wrapBook(b))
	if ok {
		t.Error("Size should report cold for missing cache file")
	}

	cachePath := filepath.Join(dir, "1", ".sidecar", "kepub.epub")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("kepub-data"), 0644); err != nil {
		t.Fatal(err)
	}

	size, ok := c.Size(wrapBook(b))
	if !ok {
		t.Fatal("Size should report hot after cache file created")
	}
	if size != 10 { // len("kepub-data")
		t.Errorf("Size = %d, want 10", size)
	}
}

// A cache at least as new as the book's DateModified is fresh.
func TestCacheEnsure(t *testing.T) {
	for _, tc := range []struct {
		name         string
		cached       bool
		modified     time.Time
		wantConverts int
		want         string
	}{
		{"stale converts", false, time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), 1, "converted"},
		{"fresh is left", true, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), 0, "fresh-cache"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, dir := newTestCache(t, "converted")
			converts := 0
			convert := c.convertFn
			c.convertFn = func(ctx context.Context, w io.Writer, r io.ReaderAt, n int64) error {
				converts++
				return convert(ctx, w, r, n)
			}

			cachePath := filepath.Join(dir, "1", ".sidecar", "kepub.epub")
			if tc.cached {
				if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cachePath, []byte("fresh-cache"), 0644); err != nil {
					t.Fatal(err)
				}
			}

			b := makeBook(1, "Test", "Alice")
			b.Meta.DateModified = tc.modified
			b.EpubSize = int64(len("epub-data"))
			if err := c.Ensure(wrapBook(b)); err != nil {
				t.Fatalf("Ensure: %v", err)
			}

			if converts != tc.wantConverts {
				t.Errorf("conversions = %d, want %d", converts, tc.wantConverts)
			}
			if data, err := os.ReadFile(cachePath); err != nil || string(data) != tc.want {
				t.Errorf("cache content = %q (err %v), want %q", data, err, tc.want)
			}
		})
	}
}
