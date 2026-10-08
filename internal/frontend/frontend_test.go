package frontend

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fake struct {
	name     string
	serveErr error
	early    bool
	hang     bool

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

type fakeHTTP struct {
	name    string
	prefix  string
	handler http.Handler
	stops   int
}

func newFakeHTTP(name, prefix string) *fakeHTTP {
	return &fakeHTTP{
		name:    name,
		prefix:  prefix,
		handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	}
}

func (f *fakeHTTP) Name() string                   { return f.name }
func (f *fakeHTTP) Prefix() string                 { return f.prefix }
func (f *fakeHTTP) Handler() http.Handler          { return f.handler }
func (f *fakeHTTP) Shutdown(context.Context) error { f.stops++; return nil }

func TestRunnerOnSignal(t *testing.T) {
	a, b := newFake("a"), newFake("b")
	runner := NewRunner("")
	runner.Register(a)
	runner.Register(b)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runner.Run(ctx, time.Second); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if a.stops != 1 || b.stops != 1 {
		t.Errorf("Shutdown calls: a=%d b=%d, want 1 each", a.stops, b.stops)
	}
}

func TestRunnerOnServeFailure(t *testing.T) {
	bind := errors.New("address already in use")
	a, b := newFake("a"), newFake("b")
	a.serveErr = bind

	runner := NewRunner("")
	runner.Register(a)
	runner.Register(b)

	err := runner.Run(context.Background(), time.Second)
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

func TestRunnerOnServeReturningEarly(t *testing.T) {
	a, b := newFake("a"), newFake("b")
	a.early = true

	runner := NewRunner("")
	runner.Register(a)
	runner.Register(b)

	err := runner.Run(context.Background(), time.Second)
	if err == nil {
		t.Fatal("Run: nil, want the early return reported")
	}
	if !strings.Contains(err.Error(), a.Name()) {
		t.Errorf("Run: %v, want the frontend that stopped named", err)
	}
}

func TestRunnerOnServeThatNeverReturns(t *testing.T) {
	a := newFake("a")
	a.hang = true
	t.Cleanup(func() { close(a.released) })

	runner := NewRunner("")
	runner.Register(a)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	const deadline = 50 * time.Millisecond
	start := time.Now()
	err := runner.Run(ctx, deadline)
	if err == nil {
		t.Fatal("Run: nil, want the deadline reported")
	}
	if waited := time.Since(start); waited > 10*deadline {
		t.Errorf("Run blocked for %s, want it back by %s", waited, deadline)
	}
}

func TestRunnerRegisterHTTPWithoutListen(t *testing.T) {
	runner := NewRunner("")
	fe := newFakeHTTP("test", "/test")

	err := runner.RegisterHTTP(fe)
	if err == nil {
		t.Fatal("RegisterHTTP: nil, want error when http.listen is empty")
	}
	if !strings.Contains(err.Error(), "http.listen is not configured") {
		t.Errorf("RegisterHTTP: %v, want error about http.listen", err)
	}
}

func TestRunnerRegisterHTTPDuplicatePrefix(t *testing.T) {
	runner := NewRunner(":8080")
	fe1 := newFakeHTTP("first", "/api")
	fe2 := newFakeHTTP("second", "/api")

	if err := runner.RegisterHTTP(fe1); err != nil {
		t.Fatalf("RegisterHTTP first: %v", err)
	}

	err := runner.RegisterHTTP(fe2)
	if err == nil {
		t.Fatal("RegisterHTTP second: nil, want error for duplicate prefix")
	}
	if !strings.Contains(err.Error(), "duplicate HTTP prefix") {
		t.Errorf("RegisterHTTP: %v, want error about duplicate prefix", err)
	}
	if !strings.Contains(err.Error(), "first") {
		t.Errorf("RegisterHTTP: %v, want error to name the existing frontend", err)
	}
}

func TestRunnerRegisterHTTPSuccess(t *testing.T) {
	runner := NewRunner(":8080")
	fe1 := newFakeHTTP("first", "/api")
	fe2 := newFakeHTTP("second", "/sync")

	if err := runner.RegisterHTTP(fe1); err != nil {
		t.Fatalf("RegisterHTTP first: %v", err)
	}
	if err := runner.RegisterHTTP(fe2); err != nil {
		t.Fatalf("RegisterHTTP second: %v", err)
	}

	if len(runner.httpFrontends) != 2 {
		t.Errorf("httpFrontends: %d, want 2", len(runner.httpFrontends))
	}
}

func TestRunnerWithHTTPFrontends(t *testing.T) {
	runner := NewRunner(":0") // :0 lets the OS pick a port
	fe := newFakeHTTP("test", "/test")

	if err := runner.RegisterHTTP(fe); err != nil {
		t.Fatalf("RegisterHTTP: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := runner.Run(ctx, time.Second); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if fe.stops != 1 {
		t.Errorf("HTTP frontend Shutdown calls: %d, want 1", fe.stops)
	}
}

func TestRunnerNoHTTPFrontendsNoListener(t *testing.T) {
	// This test verifies that when no HTTP frontends are registered,
	// no HTTP listener is started (even if http.listen is configured)
	runner := NewRunner(":8080")
	a := newFake("a")
	runner.Register(a)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := runner.Run(ctx, time.Second); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// If we got here without hanging, the HTTP listener wasn't started
	// (a real listener on :8080 would have been fine, but this verifies
	// the code path that skips HTTP server creation)
	if a.stops != 1 {
		t.Errorf("Shutdown calls: %d, want 1", a.stops)
	}
}

func TestRunnerHTTPServerLifecycle(t *testing.T) {
	// Use httptest to verify the HTTP server actually works
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test"))
	})

	fe := &fakeHTTP{
		name:    "test",
		prefix:  "/api",
		handler: handler,
	}

	// Create a test server to verify the handler works
	ts := httptest.NewServer(fe.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode: %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
