// Package fstest is the vocabulary the 9P tests assert in. A 9P operation is
// several calls against a fid, and spelling that out at every call site buries
// the claim a test is making under protocol mechanics.
//
// Every helper fails the test on an unexpected error, so a test where the error
// is the subject calls the node API directly instead. CloseErr is the one
// exception.
//
// The open mode stays an explicit argument. proto.Oread is Mode(0), and the
// distinction that matters is Otrunc, under which a field file starts its write
// buffer empty rather than seeding it from the current value.
//
// It depends only on go9p, never on internal/fs, so any package under there can
// use it.
package fstest

import (
	"slices"
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
)

type Handle struct {
	t   testing.TB
	f   fs.File
	num uint64
}

// Fid performs no 9P call, so a test can name a fid before opening it, or name
// one it never opens.
func Fid(t testing.TB, f fs.File, num uint64) *Handle {
	return &Handle{t: t, f: f, num: num}
}

func (h *Handle) Open(mode proto.Mode) {
	h.t.Helper()
	if err := h.f.Open(h.num, mode); err != nil {
		h.t.Fatalf("Open fid %d: %v", h.num, err)
	}
}

func (h *Handle) Write(off uint64, data string) {
	h.t.Helper()
	if _, err := h.f.Write(h.num, off, []byte(data)); err != nil {
		h.t.Fatalf("Write fid %d at %d: %v", h.num, off, err)
	}
}

// Close clunks the fid, which is the moment a buffered write commits.
func (h *Handle) Close() {
	h.t.Helper()
	if err := h.f.Close(h.num); err != nil {
		h.t.Fatalf("Close fid %d: %v", h.num, err)
	}
}

func (h *Handle) Read(off, count uint64) string {
	h.t.Helper()
	data, err := h.f.Read(h.num, off, count)
	if err != nil {
		h.t.Fatalf("Read fid %d at %d: %v", h.num, off, err)
	}
	return string(data)
}

func (h *Handle) Set(mode proto.Mode, data string) {
	h.t.Helper()
	h.Open(mode)
	h.Write(0, data)
	h.Close()
}

// Get leaves the fid open, since a test proving the snapshot is held reads it
// again.
func (h *Handle) Get(mode proto.Mode, count uint64) string {
	h.t.Helper()
	h.Open(mode)
	return h.Read(0, count)
}

// A read fails on an unopened or clunked fid.
func (h *Handle) WantReadError() {
	h.t.Helper()
	if data, err := h.f.Read(h.num, 0, 64); err == nil {
		h.t.Errorf("Read fid %d succeeded, want an error; got %q", h.num, data)
	}
}

// CloseErr is the only method here that never fails the test, so it is the one
// a goroutine other than the test's can call.
func (h *Handle) CloseErr() error {
	return h.f.Close(h.num)
}

func (h *Handle) WantCloseError() error {
	h.t.Helper()
	err := h.f.Close(h.num)
	if err == nil {
		h.t.Fatalf("Close fid %d succeeded, want the commit rejected", h.num)
	}
	return err
}

// ChildNames sorts, so a comparison does not depend on map order.
func ChildNames(d fs.Dir) []string {
	names := make([]string, 0, len(d.Children()))
	for name := range d.Children() {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func HasChild(t testing.TB, d fs.Dir, name string) {
	t.Helper()
	if _, ok := d.Children()[name]; !ok {
		t.Errorf("no entry %q; listing is %v", name, ChildNames(d))
	}
}

func NoChild(t testing.TB, d fs.Dir, name string) {
	t.Helper()
	if _, ok := d.Children()[name]; ok {
		t.Errorf("entry %q survived; listing is %v", name, ChildNames(d))
	}
}

func ChildCount(t testing.TB, d fs.Dir, want int) {
	t.Helper()
	if got := len(d.Children()); got != want {
		t.Errorf("listing holds %d entries, want %d: %v", got, want, ChildNames(d))
	}
}

// ChildAs returns d's entry called name as T. The type argument resolves in the
// calling package, so this reaches types fstest cannot name.
func ChildAs[T fs.FSNode](t testing.TB, d fs.Dir, name string) T {
	t.Helper()
	var zero T
	child, ok := d.Children()[name]
	if !ok {
		t.Fatalf("no entry %q; listing is %v", name, ChildNames(d))
		return zero
	}
	typed, ok := any(child).(T)
	if !ok {
		t.Fatalf("entry %q is %T, want %T", name, child, zero)
		return zero
	}
	return typed
}

func StatLength(t testing.TB, f fs.FSNode, want uint64) {
	t.Helper()
	if got := f.Stat().Length; got != want {
		t.Errorf("Stat().Length = %d, want %d", got, want)
	}
}
