// Package frontend runs the library's network frontends for the life of the
// process: start each one, wait for a signal or a failure, stop them all.
//
// A frontend that stops serving takes the others down with it, for the reasons
// in DECISIONS.md #26.
package frontend

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Frontend is a server that binds a port and serves the library until it is
// shut down.
type Frontend interface {
	// Name labels the frontend in logs and in the errors Run reports.
	Name() string

	// Serve blocks until Shutdown, then reports nil. Returning any earlier is
	// a failure whatever it reports, since the port has stopped answering.
	Serve() error

	// Shutdown closes the listener and waits out in-flight work against ctx's
	// deadline. Serve returns once it has.
	Shutdown(ctx context.Context) error
}

// Run starts every frontend and blocks until ctx is cancelled or one of them
// returns. It shuts all of them down either way, with timeout shared across
// the whole set, and reports every failure it collected.
func Run(ctx context.Context, timeout time.Duration, frontends ...Frontend) error {
	exits := make(chan exit, len(frontends))
	for _, f := range frontends {
		go func() { exits <- exit{f.Name(), f.Serve()} }()
	}

	// errors.Join drops nils, so every outcome is appended unconditionally.
	var errs []error
	running := len(frontends)

	select {
	case <-ctx.Done():
		slog.Info("shutting down…")
	case e := <-exits:
		running--
		errs = append(errs, e.early())
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for _, f := range frontends {
		if err := f.Shutdown(stopCtx); err != nil {
			errs = append(errs, fmt.Errorf("stopping %s: %w", f.Name(), err))
		}
	}

	// The wait for the remaining Serves shares the shutdown deadline. One that
	// never returns would otherwise hang the process with its listener already
	// closed, and there is nothing left to wait for at that point.
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

// exit is one Serve return, named so an error says which frontend produced it.
type exit struct {
	name string
	err  error
}

// early describes a Serve that returned before anything asked it to, which is
// a failure even when it reports nil.
func (e exit) early() error {
	if e.err == nil {
		return fmt.Errorf("%s stopped serving before shutdown", e.name)
	}
	return e.wrapped()
}

// wrapped describes a Serve that returned after Shutdown, where nil is the
// outcome the interface asks for.
func (e exit) wrapped() error {
	if e.err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", e.name, e.err)
}
