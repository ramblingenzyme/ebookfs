// Package opds is the composition root of the OPDS frontend: it wires a
// catalog onto github.com/ophymx/opds's HTTP handler and serves it. The
// handler owns feed routing, OPDS 1.2/2.0 content negotiation, OpenSearch and
// pagination. This package supplies the catalog behind it and the two byte
// routes it does not cover.
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

// SetupServer builds the catalog and its mux without binding a port, so the
// wiring can be tested with httptest. baseURL is the catalog's canonical
// absolute URL, scheme://host with no trailing slash. An empty one leaves the
// handler deriving a base per request from client-controlled headers, which is
// safe only behind a proxy that overwrites them.
func SetupServer(lib Library, rend Renderer, baseURL string) *Server {
	return &Server{base: baseURL, http: &http.Server{
		Handler: NewHandler(lib, rend, baseURL),
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
	c := &catalog{lib: lib, filename: rend.Filename, base: strings.TrimSuffix(baseURL, "/")}
	h := opdshttp.New(c,
		opdshttp.WithPrefix(Prefix),
		opdshttp.WithBaseURL(baseURL),
		opdshttp.WithDefaultVersion(opds.Version1),
	)

	mux := http.NewServeMux()
	mux.Handle(Prefix, h)
	mux.Handle(Prefix+"/", h)

	// The trailing filename segment is ignored. It is there so a client that
	// names the download after its URL rather than Content-Disposition still
	// saves a sensibly named file.
	mux.HandleFunc("GET "+contentBase+"{id}/{filename}", files.byID(files.serveEpub))
	mux.HandleFunc("GET "+coverBase+"{id}", files.byID(files.serveCover))
	return mux
}

// Start blocks, so it runs in a goroutine and the main one takes signals. It
// returns nil on a clean shutdown, as fs.Server.Start does.
func (s *Server) Start(listen string) error {
	slog.Info("serving OPDS", "listen", listen, "prefix", Prefix, "base_url", s.base)
	if s.base == "" {
		slog.Warn("opds.base_url is unset; absolute URLs in served documents follow the client's Host and X-Forwarded-* headers")
	}
	s.http.Addr = listen
	if err := s.http.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}
