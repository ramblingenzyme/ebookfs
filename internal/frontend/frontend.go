// Package frontend runs the network frontends. One that stops serving takes
// the others down with it (docs/DECISIONS.md #26).
package frontend

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type Frontend interface {
	Name() string

	// Serve blocks until Shutdown. An earlier return is a failure, even nil.
	Serve() error

	Shutdown(ctx context.Context) error
}

// Run stops every frontend once ctx is cancelled or any Serve returns. timeout
// covers the whole shutdown.
func Run(ctx context.Context, timeout time.Duration, frontends ...Frontend) error {
	exits := make(chan exit, len(frontends))
	for _, f := range frontends {
		go func() { exits <- exit{f.Name(), f.Serve()} }()
	}

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

	// Bounded, so a Serve that never returns cannot hang the process.
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
