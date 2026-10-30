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

// Config holds kosync configuration.
type Config struct {
	Username         string  // pre-provisioned username
	PasswordHash     string  // MD5 hash of password (lowercase hex)
	PathPrefix       string  // URL path prefix (e.g., "/sync")
	ReadingThreshold float64 // percentage to mark as "reading" (default: 0.05)
	ReadThreshold    float64 // percentage to mark as "read" (default: 0.95)
}

// Server implements both HTTPFrontend and Hook for kosync.
type Server struct {
	library.HookBase
	handler  http.Handler
	prefix   string
	registry *docRegistry
}

// New creates a kosync server and restores the document-ID index from the
// library's sidecars, before any request can be served.
func New(lib *library.Library, cfg Config) (*Server, error) {
	srv := &Server{registry: newDocRegistry()}
	lib.AddHook(srv)

	if err := srv.registry.Restore(lib); err != nil {
		return nil, fmt.Errorf("restoring kosync index: %w", err)
	}

	srv.handler = NewHandler(lib, srv.registry, cfg)
	srv.prefix = cfg.PathPrefix

	return srv, nil
}

// NewHandler creates a kosync HTTP handler.
func NewHandler(lib *library.Library, registry *docRegistry, cfg Config) http.Handler {
	mux := http.NewServeMux()
	h := newHandler(lib, registry, cfg)
	h.registerRoutes(mux)
	return logRequest(checkAcceptHeader(mux))
}

// Name returns the frontend name.
func (s *Server) Name() string { return "kosync" }

// Prefix returns the URL path prefix.
func (s *Server) Prefix() string { return s.prefix }

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler { return s.handler }

// Shutdown is a no-op for kosync.
func (s *Server) Shutdown(ctx context.Context) error { return nil }

// StripPrefix returns true because kosync uses relative routes.
func (s *Server) StripPrefix() bool { return true }

// writeAcceptError writes a 412 Precondition Failed response with the given error code and message.
func writeAcceptError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPreconditionFailed)
	fmt.Fprintf(w, `{"code":%d,"message":"%s"}`, code, message)
}

// checkAcceptHeader enforces the Accept header per spec §3.3.
// The reference server requires Accept: application/vnd.koreader.v1+json.
func checkAcceptHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept := r.Header.Get("Accept")
		if accept == "" {
			// Code 100: Accept header not set (practically unreachable)
			writeAcceptError(w, codeAcceptNotSet, "Accept header not set.")
			return
		}

		// Match pattern: application/vnd.koreader.v<version>...json
		// The spec's Lua pattern has unescaped dots, but we use proper regex.
		matches := acceptHeaderPattern.FindStringSubmatch(accept)
		if matches == nil {
			// Code 101: Invalid Accept header format
			writeAcceptError(w, codeAcceptInvalid, "Invalid Accept header format.")
			return
		}

		// Extract version number
		if matches[1] != "1" {
			// Code 102: Unsupported version
			writeAcceptError(w, codeAcceptVersionUnsup, "Unsupported version specified in the Accept header.")
			return
		}

		next.ServeHTTP(w, r)
	})
}

var acceptHeaderPattern = regexp.MustCompile(`^application/vnd\.koreader\.v(\d+).*json$`)

// statusWriter captures the status code written by the handler.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// logRequest logs method, path, status, duration, and authenticated user.
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

// writeError writes a JSON error response with the given status code, error code, and message.
func (h *handler) writeError(w http.ResponseWriter, status, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"code":%d,"message":"%s"}`, code, message)
}

// writeJSON writes a JSON response with the given status code and data.
func (h *handler) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeServerError logs an error and writes a 502 response with codeUnknownServer.
func (h *handler) writeServerError(w http.ResponseWriter, msg string, bookID int64, err error) {
	slog.Error(msg, "book_id", bookID, "error", err)
	h.writeError(w, http.StatusBadGateway, codeUnknownServer, "Unknown server error.")
}

// registerRoutes sets up the kosync HTTP endpoints using Go 1.22+ method-specific routing.
// PUT /syncs/progress reads document from the request body (per spec §5.4).
// GET /syncs/progress/{docID} reads document from the URL path (per spec §5.5).
// Routes are relative — the caller mounts the handler at the configured prefix.
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
		h.writeError(w, http.StatusForbidden, codeDocumentMissing, "Field 'document' not provided.")
		return false
	}
	return true
}

func (h *handler) healthcheck(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]string{"state": "OK"})
}

func (h *handler) auth(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(r) {
		h.writeError(w, http.StatusUnauthorized, codeUnauthorized, "Unauthorized")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"authorized": "OK"})
}

func (h *handler) usersCreate(w http.ResponseWriter, r *http.Request) {
	h.writeError(w, http.StatusPaymentRequired, codeRegistrationOff, "User registration is disabled.")
}

func (h *handler) getProgress(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(r) {
		h.writeError(w, http.StatusUnauthorized, codeUnauthorized, "Unauthorized")
		return
	}

	docID := r.PathValue("docID")
	if !h.validateDocumentID(w, docID) {
		return
	}

	progress, found, err := h.registry.ReadProgress(h.lib, docID)
	if err != nil {
		h.writeServerError(w, "kosync: failed to read sidecar progress", 0, err)
		return
	}
	if !found {
		h.writeJSON(w, http.StatusOK, map[string]any{})
		return
	}

	resp := struct {
		Document string `json:"document"`
		Progress
	}{
		Document: docID,
		Progress: progress,
	}
	h.writeJSON(w, http.StatusOK, resp)
}

func (h *handler) putProgress(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(r) {
		h.writeError(w, http.StatusUnauthorized, codeUnauthorized, "Unauthorized")
		return
	}

	// Parse request body to get document ID and progress data
	var req struct {
		Document   string   `json:"document"`
		Percentage *float64 `json:"percentage"`
		Progress   *string  `json:"progress"`
		Device     *string  `json:"device"`
		DeviceID   string   `json:"device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, codeJSONParseError, "Could not parse JSON in body.")
		return
	}

	docID := req.Document
	if !h.validateDocumentID(w, docID) {
		return
	}

	// Validate required fields per [K-PUT-2]
	if req.Percentage == nil || req.Progress == nil || req.Device == nil {
		h.writeError(w, http.StatusForbidden, codeInvalidRequest, "Invalid request")
		return
	}

	bookID, timestamp, found, err := h.registry.StoreProgress(h.lib, docID, Progress{
		Percentage: *req.Percentage,
		Progress:   *req.Progress,
		Device:     *req.Device,
		DeviceID:   req.DeviceID,
	})
	if err != nil {
		h.writeServerError(w, "kosync: failed to update sidecar progress", bookID, err)
		return
	}
	if !found {
		// Unknown document: accept but don't store
		h.writeJSON(w, http.StatusOK, map[string]any{
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

	h.writeJSON(w, http.StatusOK, map[string]any{
		"document":  docID,
		"timestamp": timestamp,
	})
}

// updateBookStatus updates the book's reading status based on progress percentage.
// Transitions are configurable via ReadingThreshold and ReadThreshold.
// Once a book has left "unread", it never returns to it. Once a book is "read",
// it stays "read" even if percentage drops.
func (h *handler) updateBookStatus(bookID int64, percentage float64) error {
	// Get current book
	b, err := h.lib.Get(bookID)
	if err != nil {
		return fmt.Errorf("getting book: %w", err)
	}

	// Determine target status based on configurable thresholds
	var targetStatus string
	switch {
	case percentage >= h.cfg.ReadThreshold:
		targetStatus = book.StatusRead
	case percentage >= h.cfg.ReadingThreshold:
		targetStatus = book.StatusReading
	default:
		targetStatus = book.StatusUnread
	}

	// Don't revert to "unread" once the book has left it
	if targetStatus == book.StatusUnread && b.Status() != book.StatusUnread {
		return nil
	}

	// Don't downgrade from "read" to anything else
	if b.Status() == book.StatusRead && targetStatus != book.StatusRead {
		return nil
	}

	// Only update if status changed
	if b.Status() == targetStatus {
		return nil
	}

	// Update book status
	edits := library.Edits{
		Status: &targetStatus,
	}
	if _, err := h.lib.Edit(bookID, edits); err != nil {
		return fmt.Errorf("editing book: %w", err)
	}

	slog.Info("kosync: updated book status", "book_id", bookID, "status", targetStatus, "percentage", percentage)
	return nil
}
