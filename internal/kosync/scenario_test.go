// Kosync integration scenarios verify the end-to-end flow: HTTP progress
// updates trigger sidecar writes and book status changes.
package kosync

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// TestProgressTransitions verifies that PUT progress at various percentages
// transitions the book status correctly, with configurable thresholds.
func TestProgressTransitions(t *testing.T) {
	tests := []struct {
		name  string
		cfg   Config
		steps []struct {
			percentage float64
			wantStatus string
		}
	}{
		{
			name: "default thresholds",
			cfg:  defaultCfg,
			steps: []struct {
				percentage float64
				wantStatus string
			}{
				{0.5, "reading"},
				{0.96, "read"},
				{0.1, "read"}, // no downgrade from read
			},
		},
		{
			name: "custom thresholds",
			cfg: Config{
				Username:         "testuser",
				PasswordHash:     "testhash",
				PathPrefix:       "/sync",
				ReadingThreshold: 0.10,
				ReadThreshold:    0.80,
			},
			steps: []struct {
				percentage float64
				wantStatus string
			}{
				{0.05, "unread"},
				{0.15, "reading"},
				{0.85, "read"},
			},
		},
		{
			name: "boundary values",
			cfg:  defaultCfg,
			steps: []struct {
				percentage float64
				wantStatus string
			}{
				{0.05, "reading"},
				{0.95, "read"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setupEnv(t, tt.cfg)
			for _, step := range tt.steps {
				body := map[string]any{
					"document":   env.docID,
					"percentage": step.percentage,
					"progress":   "/body/chapter1",
					"device":     "test-device",
				}
				_, code := env.putProgress(t, body)
				if code != http.StatusOK {
					t.Fatalf("PUT %v%%: status = %d, want %d", step.percentage*100, code, http.StatusOK)
				}
				if got := env.bookStatus(t); got != step.wantStatus {
					t.Errorf("book status at %v%% = %q, want %q", step.percentage*100, got, step.wantStatus)
				}
			}
		})
	}
}

// TestKosyncConcurrentProgressUpdates verifies that concurrent PUT requests
// to the same book don't corrupt the sidecar.
func TestKosyncConcurrentProgressUpdates(t *testing.T) {
	env := setupEnv(t, defaultCfg)

	const numRequests = 10
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(percentage float64) {
			body := map[string]any{
				"document":   env.docID,
				"percentage": percentage,
				"progress":   "/body/chapter",
				"device":     "test-device",
			}
			_, code := env.putProgress(t, body)
			if code != http.StatusOK {
				errors <- fmt.Errorf("request failed with status %d", code)
				return
			}
			errors <- nil
		}(float64(i+1) / 10.0)
	}

	for i := 0; i < numRequests; i++ {
		if err := <-errors; err != nil {
			t.Errorf("concurrent request %d failed: %v", i, err)
		}
	}

	// Verify the book can still be read and sidecar is valid
	_, err := env.lib.Get(env.book.ID())
	if err != nil {
		t.Fatalf("Get after concurrent updates: %v", err)
	}

	sidecarData, err := env.lib.Library.ReadSidecar(env.book.ID(), "kosync.json")
	if err != nil {
		t.Fatalf("ReadSidecar after concurrent updates: %v", err)
	}

	var sidecar SidecarData
	if err := json.Unmarshal(sidecarData, &sidecar); err != nil {
		t.Errorf("sidecar corrupted after concurrent updates: %v", err)
	}

	if sidecar.Progress.Percentage < 0 || sidecar.Progress.Percentage > 1 {
		t.Errorf("invalid percentage after concurrent updates: %v", sidecar.Progress.Percentage)
	}
}

// TestGetProgressUnknownDocument verifies GET on an unknown document returns 200 {}
// per spec §5.5 [K-GET-3].
func TestGetProgressUnknownDocument(t *testing.T) {
	env := setupEnv(t, defaultCfg)

	resp, code := env.getProgress(t, "nonexistent")
	if code != http.StatusOK {
		t.Errorf("GET unknown document: status = %d, want %d", code, http.StatusOK)
	}
	if len(resp) != 0 {
		t.Errorf("GET unknown document: response = %v, want empty object", resp)
	}
}

// TestGetProgressKnownDocument verifies GET returns all stored fields including timestamp.
func TestGetProgressKnownDocument(t *testing.T) {
	env := setupEnv(t, defaultCfg)

	putBody := map[string]any{
		"document":   env.docID,
		"percentage": 0.42,
		"progress":   "/body/DocFragment[5]/body/p[3]/text().42",
		"device":     "Kobo_clara",
		"device_id":  "ABC123",
	}
	_, code := env.putProgress(t, putBody)
	if code != http.StatusOK {
		t.Fatalf("PUT: status = %d, want %d", code, http.StatusOK)
	}

	resp, code := env.getProgress(t, env.docID)
	if code != http.StatusOK {
		t.Errorf("GET: status = %d, want %d", code, http.StatusOK)
	}

	if resp["document"] != env.docID {
		t.Errorf("document = %v, want %v", resp["document"], env.docID)
	}
	if resp["percentage"].(float64) != 0.42 {
		t.Errorf("percentage = %v, want 0.42", resp["percentage"])
	}
	if resp["progress"] != "/body/DocFragment[5]/body/p[3]/text().42" {
		t.Errorf("progress = %v, want XPointer string", resp["progress"])
	}
	if resp["device"] != "Kobo_clara" {
		t.Errorf("device = %v, want Kobo_clara", resp["device"])
	}
	if resp["device_id"] != "ABC123" {
		t.Errorf("device_id = %v, want ABC123", resp["device_id"])
	}
	if _, ok := resp["timestamp"]; !ok {
		t.Error("timestamp field missing")
	}
	if resp["timestamp"].(float64) <= 0 {
		t.Errorf("timestamp = %v, want positive integer", resp["timestamp"])
	}
}

// TestPutProgressUnknownDocument verifies PUT on an unknown document accepts
// but doesn't store, returning 200 with {document, timestamp}.
func TestPutProgressUnknownDocument(t *testing.T) {
	env := setupEnv(t, defaultCfg)

	body := map[string]any{
		"document":   "unknown-doc-id",
		"percentage": 0.5,
		"progress":   "/body/chapter1",
		"device":     "test-device",
	}
	resp, code := env.putProgress(t, body)
	if code != http.StatusOK {
		t.Errorf("PUT unknown document: status = %d, want %d", code, http.StatusOK)
	}

	if resp["document"] != "unknown-doc-id" {
		t.Errorf("document = %v, want unknown-doc-id", resp["document"])
	}
	if _, ok := resp["timestamp"]; !ok {
		t.Error("timestamp field missing")
	}
	if resp["timestamp"].(float64) <= 0 {
		t.Errorf("timestamp = %v, want positive integer", resp["timestamp"])
	}
}

// TestProgressFieldRoundTrip verifies that XPointer strings and page numbers
// survive byte-for-byte per spec §7.2 [K-FLD-3].
func TestProgressFieldRoundTrip(t *testing.T) {
	env := setupEnv(t, defaultCfg)

	tests := []struct {
		name     string
		progress string
	}{
		{"XPointer", "/body/DocFragment[11]/body/div/p[7]/text().123"},
		{"page number", "56"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]any{
				"document":   env.docID,
				"percentage": 0.42,
				"progress":   tt.progress,
				"device":     "test-device",
			}
			_, code := env.putProgress(t, body)
			if code != http.StatusOK {
				t.Fatalf("PUT: status = %d, want %d", code, http.StatusOK)
			}

			resp, code := env.getProgress(t, env.docID)
			if code != http.StatusOK {
				t.Fatalf("GET: status = %d, want %d", code, http.StatusOK)
			}

			if resp["progress"] != tt.progress {
				t.Errorf("progress round-trip failed: got %v, want %v", resp["progress"], tt.progress)
			}
		})
	}
}

// TestDeviceIdRoundTrip verifies device_id is stored and returned when sent,
// and omitted when not sent per spec §7.5 [K-FLD-7].
func TestDeviceIdRoundTrip(t *testing.T) {
	env := setupEnv(t, defaultCfg)

	// PUT with device_id
	body := map[string]any{
		"document":   env.docID,
		"percentage": 0.5,
		"progress":   "/body/chapter1",
		"device":     "test-device",
		"device_id":  "UNIQUE_DEVICE_123",
	}
	_, code := env.putProgress(t, body)
	if code != http.StatusOK {
		t.Fatalf("PUT with device_id: status = %d, want %d", code, http.StatusOK)
	}

	resp, code := env.getProgress(t, env.docID)
	if code != http.StatusOK {
		t.Fatalf("GET: status = %d, want %d", code, http.StatusOK)
	}
	if resp["device_id"] != "UNIQUE_DEVICE_123" {
		t.Errorf("device_id = %v, want UNIQUE_DEVICE_123", resp["device_id"])
	}

	// PUT without device_id
	delete(body, "device_id")
	_, code = env.putProgress(t, body)
	if code != http.StatusOK {
		t.Fatalf("PUT without device_id: status = %d, want %d", code, http.StatusOK)
	}

	// GET it back - device_id should be absent
	resp, code = env.getProgress(t, env.docID)
	if code != http.StatusOK {
		t.Fatalf("GET: status = %d, want %d", code, http.StatusOK)
	}

	// The omitempty tag means empty strings are not serialized
	if _, ok := resp["device_id"]; ok {
		t.Error("device_id should be absent when not sent")
	}
}

// TestMetadataAcceptance verifies that PUT with metadata field doesn't fail
// per spec §7.6 [K-FLD-10].
func TestMetadataAcceptance(t *testing.T) {
	env := setupEnv(t, defaultCfg)

	body := map[string]any{
		"document":   env.docID,
		"percentage": 0.5,
		"progress":   "/body/chapter1",
		"device":     "test-device",
		"metadata": map[string]any{
			"filename": "test.epub",
			"title":    "Test Book",
			"authors":  "Alice",
		},
	}
	resp, code := env.putProgress(t, body)
	if code != http.StatusOK {
		t.Errorf("PUT with metadata: status = %d, want %d, body = %s", code, http.StatusOK, resp)
	}

	if resp["document"] != env.docID {
		t.Errorf("document = %v, want %v", resp["document"], env.docID)
	}
	if _, ok := resp["timestamp"]; !ok {
		t.Error("timestamp field missing")
	}
}
