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

// content is separate from catalog so a byte route cannot reach the feed
// builders, and a feed builder cannot reach the ResponseWriter.
type content struct {
	lib  Library
	rend Renderer
}

// byID resolves the {id} segment ahead of serve and turns a returned error
// into a status, so each route below runs against a book that exists and never
// writes a failure itself.
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

	// Logged rather than returned: the body is already going out, so there is
	// no status left to send.
	slog.Warn("opds: rendition size unknown, serving without ranges", "book_id", b.ID())
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
	// The zip path's extension is the only type hint the index holds; sniffing
	// covers the rest, and both agree for the jpeg and png real epubs carry.
	w.Header().Set("Content-Type", http.DetectContentType(img))
	http.ServeContent(w, r, "", b.DateModified(), bytes.NewReader(img))
	return nil
}

// httpError maps a library error onto a status. It mirrors opdshttp's own
// default mapping, which the content routes bypass by not going through the
// OPDS handler.
func httpError(w http.ResponseWriter, r *http.Request, err error) {
	if isNotFound(err) {
		http.NotFound(w, r)
		return
	}
	slog.Error("opds: request failed", "path", r.URL.Path, "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
