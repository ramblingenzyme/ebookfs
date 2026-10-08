// Package frontend runs the network frontends. One that stops serving takes
// the others down with it (docs/DECISIONS.md #26).
package frontend

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type Frontend interface {
	Name() string

	// Serve blocks until Shutdown. An earlier return is a failure, even nil.
	Serve() error

	Shutdown(ctx context.Context) error
}

// HTTPFrontend is a frontend that serves HTTP and mounts on a shared listener.
type HTTPFrontend interface {
	Name() string
	Prefix() string
	Handler() http.Handler
	Shutdown(ctx context.Context) error
	// StripPrefix reports whether the runner should strip the prefix before
	// passing requests to the handler. Frontends using relative routes
	// return true; those with absolute routes return false.
	StripPrefix() bool
}

type exit struct {
	name string
	err  error
}

func (e exit) early() error {
	if e.err == nil {
		return fmt.Errorf("%s stopped serving before shutdown", e.name)
	}
	return e.wrapped()
}

func (e exit) wrapped() error {
	if e.err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", e.name, e.err)
}

// Runner orchestrates both regular and HTTP frontends.
type Runner struct {
	frontends     []Frontend
	httpFrontends []HTTPFrontend
	httpPrefixes  map[string]string // prefix -> frontend name
	httpListen    string
}

// NewRunner creates a Runner that will serve HTTP on the given address.
func NewRunner(httpListen string) *Runner {
	return &Runner{
		httpPrefixes: make(map[string]string),
		httpListen:   httpListen,
	}
}

// Register adds a regular frontend.
func (r *Runner) Register(f Frontend) {
	r.frontends = append(r.frontends, f)
}

// RegisterHTTP adds an HTTP frontend, validating that its prefix is unique and http.listen is configured.
func (r *Runner) RegisterHTTP(fe HTTPFrontend) error {
	if r.httpListen == "" {
		return fmt.Errorf("cannot register HTTP frontend %q: http.listen is not configured", fe.Name())
	}
	prefix := fe.Prefix()
	if existing, ok := r.httpPrefixes[prefix]; ok {
		return fmt.Errorf("duplicate HTTP prefix %q (already registered by %s)", prefix, existing)
	}
	r.httpPrefixes[prefix] = fe.Name()
	r.httpFrontends = append(r.httpFrontends, fe)
	return nil
}

// Run starts all frontends and blocks until ctx is cancelled or any Serve returns.
// It then shuts down everything with the given timeout.
func (r *Runner) Run(ctx context.Context, timeout time.Duration) error {
	var httpServer *http.Server
	var httpServeErr chan error

	// Start HTTP server if there are HTTP frontends
	if len(r.httpFrontends) > 0 {
		mux := http.NewServeMux()
		for _, fe := range r.httpFrontends {
			handler := fe.Handler()
			if fe.StripPrefix() {
				handler = http.StripPrefix(fe.Prefix(), handler)
			}
			mux.Handle(fe.Prefix(), handler)
		}
		httpServer = &http.Server{
			Addr:              r.httpListen,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		httpServeErr = make(chan error, 1)
		go func() {
			slog.Info("serving HTTP", "listen", r.httpListen, "frontends", len(r.httpFrontends))
			if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
				httpServeErr <- err
			}
			close(httpServeErr)
		}()
	} else if r.httpListen != "" {
		slog.Info("HTTP listener not started: no HTTP frontends registered")
	}

	// Start regular frontends
	exits := make(chan exit, len(r.frontends))
	for _, f := range r.frontends {
		go func() { exits <- exit{f.Name(), f.Serve()} }()
	}

	var errs []error
	running := len(r.frontends)

	// Wait for context cancellation, any frontend to exit, or HTTP server to fail
	select {
	case <-ctx.Done():
		slog.Info("shutting down…")
	case e := <-exits:
		running--
		errs = append(errs, e.early())
	case err := <-httpServeErr:
		if err != nil {
			errs = append(errs, fmt.Errorf("HTTP server: %w", err))
		}
	}

	// Shutdown everything
	stopCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Shutdown HTTP server
	if httpServer != nil {
		if err := httpServer.Shutdown(stopCtx); err != nil {
			errs = append(errs, fmt.Errorf("HTTP server shutdown: %w", err))
		}
	}

	// Shutdown HTTP frontends
	for _, fe := range r.httpFrontends {
		if err := fe.Shutdown(stopCtx); err != nil {
			errs = append(errs, fmt.Errorf("stopping %s: %w", fe.Name(), err))
		}
	}

	// Shutdown regular frontends
	for _, f := range r.frontends {
		if err := f.Shutdown(stopCtx); err != nil {
			errs = append(errs, fmt.Errorf("stopping %s: %w", f.Name(), err))
		}
	}

	// Wait for regular frontends to stop
	for running > 0 {
		select {
		case e := <-exits:
			running--
			errs = append(errs, e.wrapped())
		case <-stopCtx.Done():
			errs = append(errs, fmt.Errorf("%d frontend(s) still running at the %s deadline", running, timeout))
			running = 0
		}
	}

	return errors.Join(errs...)
}
