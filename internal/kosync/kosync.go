package kosync

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// Error codes per kosync spec §6
const (
	// Framework errors (§6.2)
	codeAcceptNotSet       = 100
	codeAcceptInvalid      = 101
	codeAcceptVersionUnsup = 102
	codeJSONParseError     = 103

	// Application errors (§6.1)
	codeRedisConnection = 1000
	codeUnknownServer   = 2000
	codeUnauthorized    = 2001
	codeUsernameTaken   = 2002
	codeInvalidRequest  = 2003
	codeDocumentMissing = 2004
	codeRegistrationOff = 2005
)

type Config struct {
	Username         string
	PasswordHash     string // MD5 of the password, lowercase hex
	PathPrefix       string
	ReadingThreshold float64 // fraction of the book, 0-1, that marks it "reading"
	ReadThreshold    float64 // fraction of the book, 0-1, that marks it "read"
}

// Server is both the HTTP frontend and a library.Hook.
type Server struct {
	library.HookBase
	lib      *library.Library
	handler  http.Handler
	prefix   string
	registry *docRegistry
}

// New creates a kosync server. Registering it restores the document-ID index
// from the library's sidecars, before any request can be served.
func New(lib *library.Library, cfg Config) (*Server, error) {
	srv := &Server{lib: lib, registry: newDocRegistry()}
	if err := lib.AddHook(srv); err != nil {
		return nil, fmt.Errorf("registering kosync hook: %w", err)
	}

	srv.handler = NewHandler(lib, srv.registry, cfg)
	srv.prefix = cfg.PathPrefix

	return srv, nil
}

func NewHandler(lib *library.Library, registry *docRegistry, cfg Config) http.Handler {
	mux := http.NewServeMux()
	h := newHandler(lib, registry, cfg)
	h.registerRoutes(mux)
	return logRequest(checkAcceptHeader(mux))
}

func (s *Server) Name() string { return "kosync" }

func (s *Server) Prefix() string { return s.prefix }

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) Shutdown(ctx context.Context) error { return nil }

// StripPrefix returns true because kosync uses relative routes.
func (s *Server) StripPrefix() bool { return true }

// checkAcceptHeader enforces the Accept header per spec §3.3.
// The reference server requires Accept: application/vnd.koreader.v1+json.
func checkAcceptHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept := r.Header.Get("Accept")
		if accept == "" {
			writeError(w, http.StatusPreconditionFailed, codeAcceptNotSet, "Accept header not set.")
			return
		}

		// The spec's Lua pattern leaves its dots unescaped, so it also matches
		// any character there. This one matches them literally.
		matches := acceptHeaderPattern.FindStringSubmatch(accept)
		if matches == nil {
			writeError(w, http.StatusPreconditionFailed, codeAcceptInvalid, "Invalid Accept header format.")
			return
		}

		if matches[1] != "1" {
			writeError(w, http.StatusPreconditionFailed, codeAcceptVersionUnsup, "Unsupported version specified in the Accept header.")
			return
		}

		next.ServeHTTP(w, r)
	})
}

var acceptHeaderPattern = regexp.MustCompile(`^application/vnd\.koreader\.v(\d+).*json$`)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// logRequest logs the X-Auth-User the client sent, whether or not it
// authenticated.
func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("kosync request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration", time.Since(start),
			"user", r.Header.Get("X-Auth-User"),
		)
	})
}

type handler struct {
	lib      *library.Library
	registry *docRegistry
	cfg      Config
}

func newHandler(lib *library.Library, registry *docRegistry, cfg Config) *handler {
	return &handler{
		lib:      lib,
		registry: registry,
		cfg:      cfg,
	}
}

func writeError(w http.ResponseWriter, status, code int, message string) {
	writeJSON(w, status, struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{code, message})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeServerError logs an error and writes a 502 response with codeUnknownServer.
func writeServerError(w http.ResponseWriter, msg string, bookID int64, err error) {
	slog.Error(msg, "book_id", bookID, "error", err)
	writeError(w, http.StatusBadGateway, codeUnknownServer, "Unknown server error.")
}

// registerRoutes takes the document from the body for PUT (spec §5.4) and from
// the path for GET (§5.5). Routes are relative to the prefix the runner strips.
func (h *handler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthcheck", h.healthcheck)
	mux.HandleFunc("GET /users/auth", h.auth)
	mux.HandleFunc("POST /users/create", h.usersCreate)
	mux.HandleFunc("GET /syncs/progress/{docID}", h.getProgress)
	mux.HandleFunc("PUT /syncs/progress", h.putProgress)
}

// authenticate compares both headers in constant time and evaluates both, so
// response timing does not reveal how much of either matched.
func (h *handler) authenticate(r *http.Request) bool {
	user := subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Auth-User")), []byte(h.cfg.Username))
	key := subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Auth-Key")), []byte(h.cfg.PasswordHash))
	return user&key == 1
}

func (h *handler) validateDocumentID(w http.ResponseWriter, docID string) bool {
	if docID == "" || strings.Contains(docID, ":") {
		writeError(w, http.StatusForbidden, codeDocumentMissing, "Field 'document' not provided.")
		return false
	}
	return true
}

func (h *handler) healthcheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"state": "OK"})
}

func (h *handler) auth(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(r) {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "Unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"authorized": "OK"})
}

func (h *handler) usersCreate(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusPaymentRequired, codeRegistrationOff, "User registration is disabled.")
}

func (h *handler) getProgress(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(r) {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "Unauthorized")
		return
	}

	docID := r.PathValue("docID")
	if !h.validateDocumentID(w, docID) {
		return
	}

	bookID, progress, found, err := h.registry.ReadProgress(h.lib, docID)
	if err != nil {
		writeServerError(w, "kosync: failed to read sidecar progress", bookID, err)
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}

	resp := struct {
		Document string `json:"document"`
		Progress
	}{
		Document: docID,
		Progress: progress,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) putProgress(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(r) {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "Unauthorized")
		return
	}

	var req struct {
		Document   string   `json:"document"`
		Percentage *float64 `json:"percentage"`
		Progress   *string  `json:"progress"`
		Device     *string  `json:"device"`
		DeviceID   string   `json:"device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, codeJSONParseError, "Could not parse JSON in body.")
		return
	}

	docID := req.Document
	if !h.validateDocumentID(w, docID) {
		return
	}

	// [K-PUT-2]
	if req.Percentage == nil || req.Progress == nil || req.Device == nil {
		writeError(w, http.StatusForbidden, codeInvalidRequest, "Invalid request")
		return
	}

	bookID, timestamp, found, err := h.registry.StoreProgress(h.lib, docID, Progress{
		Percentage: *req.Percentage,
		Progress:   *req.Progress,
		Device:     *req.Device,
		DeviceID:   req.DeviceID,
	})
	if err != nil {
		writeServerError(w, "kosync: failed to update sidecar progress", bookID, err)
		return
	}
	if !found {
		// Unknown document: accept but don't store
		writeJSON(w, http.StatusOK, map[string]any{
			"document":  docID,
			"timestamp": time.Now().Unix(),
		})
		return
	}

	// Update book status based on progress percentage (outside the sidecar
	// lock). A failed status update doesn't fail the request.
	if err := h.updateBookStatus(bookID, *req.Percentage); err != nil {
		slog.Error("kosync: failed to update book status", "book_id", bookID, "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"document":  docID,
		"timestamp": timestamp,
	})
}

// updateBookStatus's guards read the status under Library.Update, so a status
// set over 9P or ctl between this PUT's lookup and its write cannot be
// downgraded.
func (h *handler) updateBookStatus(bookID int64, percentage float64) error {
	var targetStatus string
	switch {
	case percentage >= h.cfg.ReadThreshold:
		targetStatus = book.StatusRead
	case percentage >= h.cfg.ReadingThreshold:
		targetStatus = book.StatusReading
	default:
		targetStatus = book.StatusUnread
	}

	_, changed, err := h.lib.Update(bookID, func(b *library.Book) *library.Edits {
		current := b.Status()
		// An edit that sets the same status still bumps the modified time and
		// fires OnEdited, which rehashes the epub on every PUT.
		if current == targetStatus {
			return nil
		}
		// A PUT below reading_threshold also comes from a book reopened at its
		// start, so obeying it would reset a book mid-read to unread.
		if targetStatus == book.StatusUnread {
			return nil
		}
		// Rereading a finished book sends low percentages again, and would
		// otherwise demote it to reading.
		if current == book.StatusRead {
			return nil
		}
		return &library.Edits{Status: &targetStatus}
	})
	if err != nil {
		return fmt.Errorf("editing book: %w", err)
	}
	if changed {
		slog.Info("kosync: updated book status", "book_id", bookID, "status", targetStatus, "percentage", percentage)
	}
	return nil
}
