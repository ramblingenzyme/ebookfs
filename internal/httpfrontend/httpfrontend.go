package httpfrontend

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Config configures the shared HTTP frontend.
type Config struct {
	// Listen is the address to listen on (e.g., "127.0.0.1:8080").
	Listen string
}

// Server is a shared HTTP server that multiplexes handlers.
type Server struct {
	http *http.Server
	mux  *http.ServeMux
	name string
}

// New creates a shared HTTP server.
func New(cfg Config) *Server {
	mux := http.NewServeMux()
	s := &Server{
		mux:  mux,
		name: "http",
		http: &http.Server{
			Addr:              cfg.Listen,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
	}
	return s
}

// Handle registers a handler for the given pattern.
func (s *Server) Handle(pattern string, handler http.Handler) {
	s.mux.Handle(pattern, handler)
}

// Name returns the server name for the frontend interface.
func (s *Server) Name() string { return s.name }

// Serve starts the HTTP server and blocks until it shuts down.
func (s *Server) Serve() error {
	slog.Info("serving HTTP", "listen", s.http.Addr)
	if err := s.http.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}
