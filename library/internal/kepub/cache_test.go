package kepub

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

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
