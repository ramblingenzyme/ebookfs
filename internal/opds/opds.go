// Package opds is the composition root of the OPDS frontend: it wires a
// catalog onto github.com/ophymx/opds's HTTP handler and serves it. The
// handler owns feed routing, OPDS 1.2/2.0 content negotiation, OpenSearch and
// pagination; this package supplies the catalog behind it (source.go) and the
// two content routes it does not cover (content.go).
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
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// Prefix is where the catalog is mounted. It is fixed rather than configured:
// a deployment that wants it elsewhere rewrites the path in its reverse proxy,
// and every href the catalog emits is built from this one constant.
const Prefix = "/opds"

// Library is what the OPDS frontend needs of the backend: read-only queries
// plus the three facet listings its navigation feeds are built from.
type Library interface {
	Search(library.Query) ([]*library.Book, error)
	Get(int64) (*library.Book, error)
	// Content opens the original epub. Covers are read through it rather than
	// through the Renderer so serving one never triggers a conversion.
	Content(int64) (library.EpubReader, error)
	Authors() ([]library.Facet, error)
	Series() ([]library.Facet, error)
	Tags() ([]library.Facet, error)
}

// Renderer is what the catalog needs of an exporter to deliver a book's bytes.
// library.Exporter's Includes and Dirname are deliberately absent: they decide
// reader/ membership and grouping, and the catalog serves the whole library
// under its own navigation. Open, Size and Filename never consult them.
type Renderer interface {
	Open(*library.Book) (library.EpubReader, error)
	Size(*library.Book) (int64, bool)
	Filename(*library.Book) string
}

// Server wraps an http.Server serving the catalog.
type Server struct {
	http *http.Server
	// base is kept only to log it at startup. An empty one means the absolute
	// URLs in served documents come from client headers, which is the setting
	// an operator chasing wrong links needs to see.
	base string
}

// SetupServer builds the catalog and its mux without binding a port, so the
// wiring can be tested with httptest. baseURL is the catalog's canonical
// absolute URL (scheme://host, no trailing slash); when empty, the handler
// derives one per request from client-controlled headers, which is only safe
// behind a proxy that overwrites them.
func SetupServer(lib Library, rend Renderer, baseURL string) *Server {
	return &Server{base: baseURL, http: &http.Server{
		Handler: NewHandler(lib, rend, baseURL),
		// No WriteTimeout: a response is a whole epub, and a slow client on a
		// slow link would have its download cut off mid-file.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}}
}

// NewHandler returns the catalog's http.Handler: the OPDS handler at Prefix,
// with the two content routes registered ahead of it. ServeMux prefers the
// more specific pattern, so the content routes win over the catalog's
// subtree match.
func NewHandler(lib Library, rend Renderer, baseURL string) http.Handler {
	c := &catalog{lib: lib, rend: rend, base: strings.TrimSuffix(baseURL, "/")}
	h := opdshttp.New(c,
		opdshttp.WithPrefix(Prefix),
		opdshttp.WithBaseURL(baseURL),
		opdshttp.WithDefaultVersion(opds.Version1),
	)

	mux := http.NewServeMux()
	mux.Handle(Prefix, h)
	mux.Handle(Prefix+"/", h)
	// The trailing filename segment is ignored: it is there so a client that
	// names the download after its URL rather than Content-Disposition still
	// saves a sensibly named file.
	mux.HandleFunc("GET "+Prefix+"/content/{id}/{filename}", c.serveEpub)
	mux.HandleFunc("GET "+Prefix+"/cover/{id}", c.serveCover)
	return mux
}

// Start serves the catalog on listen and blocks until Shutdown is called. It
// should be called from a background goroutine; the main goroutine handles
// signals. Like fs.Server.Start, it returns nil on a clean shutdown.
func (s *Server) Start(listen string) error {
	s.http.Addr = listen
	slog.Info("serving OPDS", "listen", listen, "prefix", Prefix, "base_url", s.base)
	if s.base == "" {
		slog.Warn("opds.base_url is unset; absolute URLs in served documents follow the client's Host and X-Forwarded-* headers")
	}
	if err := s.http.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown closes the listener and waits for in-flight requests, bounded by
// ctx.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}
