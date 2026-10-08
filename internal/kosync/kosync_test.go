package kosync

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	cfg := Config{
		Username:     "testuser",
		PasswordHash: "testpass",
		PathPrefix:   "/sync",
	}
	handler := NewHandler(nil, nil, cfg)

	req := httptest.NewRequest("GET", "/sync/healthcheck", nil)
	req.Header.Set("Accept", "application/vnd.koreader.v1+json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("healthcheck status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["state"] != "OK" {
		t.Errorf("healthcheck state = %q, want %q", resp["state"], "OK")
	}
}

func TestAuth(t *testing.T) {
	cfg := Config{
		Username:     "testuser",
		PasswordHash: "testpass",
		PathPrefix:   "/sync",
	}
	handler := NewHandler(nil, nil, cfg)

	tests := []struct {
		name       string
		username   string
		credential string
		wantStatus int
	}{
		{"valid credentials", "testuser", "testpass", http.StatusOK},
		{"wrong username", "wronguser", "testpass", http.StatusUnauthorized},
		{"wrong credential", "testuser", "wrongpass", http.StatusUnauthorized},
		{"missing headers", "", "", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/sync/users/auth", nil)
			req.Header.Set("Accept", "application/vnd.koreader.v1+json")
			if tt.username != "" {
				req.Header.Set("x-auth-user", tt.username)
			}
			if tt.credential != "" {
				req.Header.Set("x-auth-key", tt.credential)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("auth status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				var resp map[string]string
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("unmarshal response: %v", err)
				}
				if resp["authorized"] != "OK" {
					t.Errorf("auth authorized = %q, want %q", resp["authorized"], "OK")
				}
			}
		})
	}
}

func TestRegister(t *testing.T) {
	cfg := Config{
		Username:     "testuser",
		PasswordHash: "testpass",
		PathPrefix:   "/sync",
	}
	handler := NewHandler(nil, nil, cfg)

	req := httptest.NewRequest("POST", "/sync/users/create", nil)
	req.Header.Set("Accept", "application/vnd.koreader.v1+json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusPaymentRequired {
		t.Errorf("register status = %d, want %d", w.Code, http.StatusPaymentRequired)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["message"] != "User registration is disabled." {
		t.Errorf("register message = %v, want 'User registration is disabled.'", resp["message"])
	}
	if resp["code"] != 2005.0 {
		t.Errorf("register code = %v, want 2005", resp["code"])
	}
}
