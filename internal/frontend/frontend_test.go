package frontend

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fake blocks in Serve until Shutdown releases it. A serveErr makes Serve
// return straight away instead, standing in for a listener that cannot bind.
type fake struct {
	name     string
	serveErr error
	early    bool // Serve returns nil without waiting for Shutdown
	hang     bool // Serve stays blocked even after Shutdown

	released chan struct{}
	stops    int // written by Run's goroutine, read once it has returned
}

func newFake(name string) *fake {
	return &fake{name: name, released: make(chan struct{})}
}

func (f *fake) Name() string { return f.name }

func (f *fake) Serve() error {
	if f.serveErr != nil || f.early {
		return f.serveErr
	}
	<-f.released
	return nil
}

func (f *fake) Shutdown(context.Context) error {
	f.stops++
	if !f.hang {
		close(f.released)
	}
	return nil
}

// A cancelled context stops every frontend, and reports nothing.
func TestRunOnSignal(t *testing.T) {
	a, b := newFake("a"), newFake("b")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, time.Second, a, b); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if a.stops != 1 || b.stops != 1 {
		t.Errorf("Shutdown calls: a=%d b=%d, want 1 each", a.stops, b.stops)
	}
}

// One frontend failing stops the rest, without waiting for a signal. ctx is
// never cancelled here, so a Run that returns at all is the fail-fast path.
func TestRunOnServeFailure(t *testing.T) {
	bind := errors.New("address already in use")
	a, b := newFake("a"), newFake("b")
	a.serveErr = bind

	err := Run(context.Background(), time.Second, a, b)
	if !errors.Is(err, bind) {
		t.Fatalf("Run: %v, want %v", err, bind)
	}
	if !strings.Contains(err.Error(), a.Name()) {
		t.Errorf("Run: %v, want the failing frontend named", err)
	}
	if b.stops != 1 {
		t.Errorf("Shutdown calls on the healthy frontend: %d, want 1", b.stops)
	}
}

// A Serve returning nil before Shutdown is still a failure. Reporting nil
// would exit 0 on a frontend that quietly stopped answering its port.
func TestRunOnServeReturningEarly(t *testing.T) {
	a, b := newFake("a"), newFake("b")
	a.early = true

	err := Run(context.Background(), time.Second, a, b)
	if err == nil {
		t.Fatal("Run: nil, want the early return reported")
	}
	if !strings.Contains(err.Error(), a.Name()) {
		t.Errorf("Run: %v, want the frontend that stopped named", err)
	}
}

// The wait for a Serve that never returns ends at the shutdown deadline.
// Unbounded, this is the hang an operator sees as a process that ignores
// SIGTERM.
func TestRunOnServeThatNeverReturns(t *testing.T) {
	a := newFake("a")
	a.hang = true
	t.Cleanup(func() { close(a.released) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	const deadline = 50 * time.Millisecond
	start := time.Now()
	err := Run(ctx, deadline, a)
	if err == nil {
		t.Fatal("Run: nil, want the deadline reported")
	}
	if waited := time.Since(start); waited > 10*deadline {
		t.Errorf("Run blocked for %s, want it back by %s", waited, deadline)
	}
}
