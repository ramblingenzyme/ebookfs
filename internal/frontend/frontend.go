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

type Runner struct {
	frontends     []Frontend
	httpFrontends []HTTPFrontend
	httpPrefixes  map[string]string // prefix -> frontend name
	httpListen    string
}

func NewRunner(httpListen string) *Runner {
	return &Runner{
		httpPrefixes: make(map[string]string),
		httpListen:   httpListen,
	}
}

func (r *Runner) Register(f Frontend) {
	r.frontends = append(r.frontends, f)
}

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

func (r *Runner) httpHandler() http.Handler {
	mux := http.NewServeMux()

	for _, fe := range r.httpFrontends {
		handler := fe.Handler()
		if fe.StripPrefix() {
			handler = http.StripPrefix(fe.Prefix(), handler)
			// Stripping the bare prefix leaves an empty path, which the
			// frontend's own ServeMux redirects to the site root.
			mux.Handle(fe.Prefix(), http.RedirectHandler(fe.Prefix()+"/", http.StatusMovedPermanently))
		} else {
			mux.Handle(fe.Prefix(), handler)
		}

		mux.Handle(fe.Prefix()+"/", handler)
	}

	return mux
}

// Run starts all frontends and blocks until ctx is cancelled or any Serve returns.
// It then shuts down everything with the given timeout.
func (r *Runner) Run(ctx context.Context, timeout time.Duration) error {
	httpServer, httpServeErr := r.startHTTP()

	exits := make(chan exit, len(r.frontends))
	for _, f := range r.frontends {
		go func() { exits <- exit{f.Name(), f.Serve()} }()
	}

	var errs []error

	running := len(r.frontends)

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

	stopCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	errs = append(errs, r.shutdown(stopCtx, httpServer)...)

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

// startHTTP serves the HTTP frontends on one listener. With none registered
// it returns a nil server and a nil channel, which never delivers.
func (r *Runner) startHTTP() (*http.Server, <-chan error) {
	if len(r.httpFrontends) == 0 {
		if r.httpListen != "" {
			slog.Info("HTTP listener not started: no HTTP frontends registered")
		}

		return nil, nil
	}

	srv := &http.Server{
		Addr:              r.httpListen,
		Handler:           r.httpHandler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serveErr := make(chan error, 1)

	go func() {
		slog.Info("serving HTTP", "listen", r.httpListen, "frontends", len(r.httpFrontends))

		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			serveErr <- err
		}

		close(serveErr)
	}()

	return srv, serveErr
}

// shutdown stops the HTTP server, then the HTTP frontends, then the rest.
func (r *Runner) shutdown(ctx context.Context, httpServer *http.Server) []error {
	var errs []error

	if httpServer != nil {
		if err := httpServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("HTTP server shutdown: %w", err))
		}
	}

	for _, fe := range r.httpFrontends {
		if err := fe.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stopping %s: %w", fe.Name(), err))
		}
	}

	for _, f := range r.frontends {
		if err := f.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stopping %s: %w", f.Name(), err))
		}
	}

	return errs
}
