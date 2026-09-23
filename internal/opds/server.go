// Package opds serves the library as an OPDS catalog through
// github.com/ophymx/opds, adding the download and cover routes it lacks.
//
// The catalog is read-only. OPDS has no write semantics, so 9P remains the
// only way into the library (DECISIONS.md #4).
package opds

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ophymx/opds"
	"github.com/ophymx/opds/opdshttp"
)

type Server struct {
	http *http.Server
	base string // kept only for the startup log
}

type Config struct {
	Listen string

	// BaseURL is the catalog's canonical absolute URL, scheme://host. Trailing
	// slashes are stripped. An empty one leaves the handler deriving a base per
	// request from client-controlled headers, which is safe only behind a
	// proxy that overwrites them.
	BaseURL string
}

func New(lib Library, rend Renderer, cfg Config) *Server {
	return &Server{base: cfg.BaseURL, http: &http.Server{
		Addr:    cfg.Listen,
		Handler: NewHandler(lib, rend, cfg.BaseURL),
		// No WriteTimeout: a response is a whole epub, and a slow client on a
		// slow link would have its download cut off mid-file.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}}
}

// NewHandler mounts the OPDS handler at Prefix with the two content routes
// ahead of it. ServeMux prefers the more specific pattern, so the content
// routes win over the catalog's subtree match.
func NewHandler(lib Library, rend Renderer, baseURL string) http.Handler {
	files := &content{lib: lib, rend: rend}
	// opdshttp.WithBaseURL trims every trailing slash, so the search template
	// must too.
	baseURL = strings.TrimRight(baseURL, "/")
	c := &catalog{lib: lib, filename: rend.Filename, base: baseURL}
	h := opdshttp.New(c,
		opdshttp.WithPrefix(Prefix),
		opdshttp.WithBaseURL(baseURL),
		opdshttp.WithDefaultVersion(opds.Version1),
	)

	mux := http.NewServeMux()
	mux.Handle(Prefix, h)
	mux.Handle(Prefix+"/", h)

	// No handler reads {filename}. It is there for clients that ignore
	// Content-Disposition and name the download after the URL's last segment.
	mux.HandleFunc("GET "+contentBase+"{id}/{filename}", files.byID(files.serveEpub))
	mux.HandleFunc("GET "+coverBase+"{id}", files.byID(files.serveCover))
	return mux
}

func (s *Server) Name() string { return "OPDS" }

func (s *Server) Serve() error {
	slog.Info("serving OPDS", "listen", s.http.Addr, "prefix", Prefix, "base_url", s.base)
	if s.base == "" {
		slog.Warn("opds.base_url is unset; absolute URLs in served documents follow the client's Host and X-Forwarded-* headers")
	}
	if err := s.http.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}
