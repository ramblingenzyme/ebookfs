package opds

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"

	"github.com/ophymx/opds"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// serveEpub delivers a book's export rendition. http.ServeContent handles
// Range requests, conditional GETs and Content-Length, which is what lets a
// reader resume an interrupted download.
func (c *catalog) serveEpub(w http.ResponseWriter, r *http.Request) {
	b, err := c.book(r.PathValue("id"))
	if err != nil {
		httpError(w, r, err)
		return
	}
	rd, err := c.rend.Open(b)
	if err != nil {
		httpError(w, r, err)
		return
	}
	defer rd.Close()

	name := c.rend.Filename(b)
	w.Header().Set("Content-Type", mediaTypeEpub)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))

	// Size is cheap and, for a converted rendition, true once Open has built
	// it. The fallback covers the rendition disappearing between the two.
	if size, ok := c.rend.Size(b); ok {
		http.ServeContent(w, r, name, b.DateModified(), io.NewSectionReader(rd, 0, size))
		return
	}
	slog.Warn("opds: rendition size unknown, serving without ranges", "book", b.ID())
	if _, err := io.Copy(w, io.NewSectionReader(rd, 0, math.MaxInt64)); err != nil {
		slog.Warn("opds: download failed", "book", b.ID(), "error", err)
	}
}

// serveCover delivers a book's cover image from the original epub rather than
// the export rendition, so browsing a catalog of covers never triggers a
// conversion.
func (c *catalog) serveCover(w http.ResponseWriter, r *http.Request) {
	b, err := c.book(r.PathValue("id"))
	if err != nil {
		httpError(w, r, err)
		return
	}
	rd, err := c.lib.Content(b.ID())
	if err != nil {
		httpError(w, r, err)
		return
	}
	defer rd.Close()

	img, err := rd.Cover()
	if err != nil {
		httpError(w, r, err)
		return
	}
	// The zip path's extension is the only type hint the index holds; sniffing
	// covers the rest, and both agree for the jpeg and png real epubs carry.
	w.Header().Set("Content-Type", http.DetectContentType(img))
	http.ServeContent(w, r, "", b.DateModified(), bytes.NewReader(img))
}

// isNotFound reports whether err means the catalog should answer 404 rather
// than 500: a book the index does not hold, or one with no cover to serve.
func isNotFound(err error) bool {
	return errors.Is(err, library.ErrBookNotFound) || errors.Is(err, library.ErrNoCover)
}

// httpError maps a backend error onto a status. It mirrors opdshttp's own
// default mapping, which the content routes bypass by not going through the
// OPDS handler.
func httpError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, opds.ErrNotFound) || isNotFound(err) {
		http.NotFound(w, r)
		return
	}
	slog.Error("opds: request failed", "path", r.URL.Path, "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
