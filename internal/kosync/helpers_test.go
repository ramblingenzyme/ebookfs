package kosync

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// testLibrary wraps library.Library for test cleanup
type testLibrary struct {
	*library.Library
	tmpDir string
}

// openTestLibrary creates a test library with a temporary directory
func openTestLibrary(t *testing.T) *testLibrary {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "kosync-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}

	lib, err := library.Open(library.Config{
		Root:      tmpDir,
		IndexPath: tmpDir + "/.index.db",
		InboxTemp: tmpDir + "/.inbox-tmp",
	})
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("opening library: %v", err)
	}

	tl := &testLibrary{
		Library: lib,
		tmpDir:  tmpDir,
	}

	t.Cleanup(func() {
		lib.Close()
		os.RemoveAll(tmpDir)
	})

	return tl
}

// buildTestEpub creates a minimal valid epub for testing
func buildTestEpub(t *testing.T, title string, authors ...string) []byte {
	t.Helper()
	return util.BuildTestEpub(t, title, authors...)
}

// ingestTestEpub ingests an epub into the library
func ingestTestEpub(t *testing.T, lib *library.Library, data []byte) *library.Book {
	t.Helper()
	h, err := lib.CreateIngest()
	if err != nil {
		t.Fatalf("CreateIngest: %v", err)
	}
	if _, err := h.WriteAt(data, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	b, err := h.Ingest()
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return b
}

// testEnv bundles the recurring setup for kosync scenario tests.
type testEnv struct {
	lib     *testLibrary
	mapping *MappingFile
	handler http.Handler
	docID   string
	book    *library.Book
}

// defaultCfg is the config most scenario tests use.
var defaultCfg = Config{
	Username:         "testuser",
	PasswordHash:     "testhash",
	PathPrefix:       "/sync",
	ReadingThreshold: 0.05,
	ReadThreshold:    0.95,
}

// setupEnv creates a library, mapping, hook, one ingested book, and a handler.
func setupEnv(t *testing.T, cfg Config) *testEnv {
	t.Helper()
	lib := openTestLibrary(t)
	mapping := NewEmptyMapping(t.TempDir())
	hook := NewHook(mapping)
	lib.AddHook(hook)

	data := buildTestEpub(t, "Test Book", "Author")
	book := ingestTestEpub(t, lib.Library, data)

	var docID string
	for id := range mapping.Snapshot() {
		docID = id
		break
	}
	if docID == "" {
		t.Fatal("document ID not in mapping after ingest")
	}

	handler := NewHandler(lib.Library, mapping, cfg)
	return &testEnv{lib: lib, mapping: mapping, handler: handler, docID: docID, book: book}
}

// putProgress sends a PUT /syncs/progress with the given body.
func (e *testEnv) putProgress(t *testing.T, body map[string]any) (map[string]any, int) {
	t.Helper()
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/syncs/progress", bytes.NewReader(bodyBytes))
	req.Header.Set("Accept", "application/vnd.koreader.v1+json")
	req.Header.Set("X-Auth-User", "testuser")
	req.Header.Set("X-Auth-Key", "testhash")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, req)
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp, w.Code
}

// getProgress sends a GET /syncs/progress/{docID}.
func (e *testEnv) getProgress(t *testing.T, docID string) (map[string]any, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/syncs/progress/"+docID, nil)
	req.Header.Set("Accept", "application/vnd.koreader.v1+json")
	req.Header.Set("X-Auth-User", "testuser")
	req.Header.Set("X-Auth-Key", "testhash")
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, req)
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp, w.Code
}

// bookStatus returns the current status of the ingested book.
func (e *testEnv) bookStatus(t *testing.T) string {
	t.Helper()
	b, err := e.lib.Get(e.book.ID())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return b.Status()
}

// setupMapping creates a library, mapping, and hook (no books).
func setupMapping(t *testing.T) (*testLibrary, *MappingFile) {
	t.Helper()
	lib := openTestLibrary(t)
	mapping := NewEmptyMapping(t.TempDir())
	hook := NewHook(mapping)
	lib.AddHook(hook)
	return lib, mapping
}
