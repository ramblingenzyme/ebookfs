package opds

import (
	"bytes"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

type content struct {
	lib  Library
	rend Renderer
}

// byID turns serve's error into a status, so no route writes its own failure.
func (c *content) byID(serve func(*library.Book, http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := lookupBook(c.lib, r.PathValue("id"))
		if err == nil {
			err = serve(b, w, r)
		}
		if err != nil {
			httpError(w, r, err)
		}
	}
}

// serveEpub delivers a book's export rendition through http.ServeContent, so a
// reader can resume an interrupted download with a Range request.
func (c *content) serveEpub(b *library.Book, w http.ResponseWriter, r *http.Request) error {
	rd, err := c.rend.Open(b)
	if err != nil {
		return err
	}
	defer rd.Close()

	name := c.rend.Filename(b)
	w.Header().Set("Content-Type", mediaTypeEpub)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))

	// Size is cheap and, for a converted rendition, true once Open has built
	// it. The fallback covers the rendition disappearing between the two.
	if size, ok := c.rend.Size(b); ok {
		http.ServeContent(w, r, name, b.DateModified(), io.NewSectionReader(rd, 0, size))
		return nil
	}

	slog.Warn("opds: rendition size unknown, serving without ranges", "book_id", b.ID())
	// Logged rather than returned, since the body is already going out and
	// there is no status left to send.
	if _, err := io.Copy(w, io.NewSectionReader(rd, 0, math.MaxInt64)); err != nil {
		slog.Warn("opds: download failed", "book_id", b.ID(), "error", err)
	}
	return nil
}

// serveCover delivers a book's cover image from the original epub rather than
// the export rendition, so browsing a catalog of covers never triggers a
// conversion.
func (c *content) serveCover(b *library.Book, w http.ResponseWriter, r *http.Request) error {
	rd, err := c.lib.Content(b.ID())
	if err != nil {
		return err
	}
	defer rd.Close()

	img, err := rd.Cover()
	if err != nil {
		return err
	}
	// Sniffed rather than taken from the cover path's extension as
	// toPublication does. The two agree for the jpeg and png real epubs carry.
	w.Header().Set("Content-Type", http.DetectContentType(img))
	http.ServeContent(w, r, "", b.DateModified(), bytes.NewReader(img))
	return nil
}

// httpError mirrors opdshttp's default error mapping, which these routes bypass.
func httpError(w http.ResponseWriter, r *http.Request, err error) {
	if isNotFound(err) {
		http.NotFound(w, r)
		return
	}
	slog.Error("opds: request failed", "path", r.URL.Path, "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
