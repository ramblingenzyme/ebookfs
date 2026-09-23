package opds

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
)

// The catalog trimmed one trailing slash where opdshttp trims all of them, so
// a base ending in "//" put a doubled slash into the search template.
func TestNewHandlerStripsTrailingSlashesFromBaseURL(t *testing.T) {
	h := NewHandler(&mock.Catalog{}, mock.Exporter{}, "https://books.example.com//")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, Prefix+"/opensearch.xml", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("opensearch: status %d", w.Code)
	}
	if want := "https://books.example.com" + Prefix + "/search?q={searchTerms}"; !strings.Contains(w.Body.String(), want) {
		t.Errorf("opensearch template is not %q:\n%s", want, w.Body)
	}
}
