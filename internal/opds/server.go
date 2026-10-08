// Package opds serves the library as an OPDS catalog through
// github.com/ophymx/opds, adding the download and cover routes it lacks.
//
// The catalog is read-only. OPDS has no write semantics, so 9P remains the
// only way into the library (docs/DECISIONS.md #4).
package opds

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ophymx/opds"
	"github.com/ophymx/opds/opdshttp"
)

type Server struct {
	handler http.Handler
	base    string // kept only for the startup log
}

type Config struct {
	// BaseURL is the catalog's canonical absolute URL, scheme://host. Trailing
	// slashes are stripped. An empty one leaves the handler deriving a base per
	// request from client-controlled headers, which is safe only behind a
	// proxy that overwrites them.
	BaseURL string
}

func New(lib Library, rend Renderer, cfg Config) *Server {
	handler := NewHandler(lib, rend, cfg.BaseURL)
	if cfg.BaseURL == "" {
		slog.Warn("opds.base_url is unset; absolute URLs in served documents follow the client's Host and X-Forwarded-* headers")
	}
	return &Server{
		handler: handler,
		base:    cfg.BaseURL,
	}
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

func (s *Server) Prefix() string { return Prefix }

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) Shutdown(ctx context.Context) error {
	return nil
}
