package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/femi/golang-easyrent/internal/auth"
	"github.com/femi/golang-easyrent/internal/favorite"
	"github.com/femi/golang-easyrent/internal/listing"
	"github.com/femi/golang-easyrent/internal/ratelimit"
)

func scalarTestHandler() Handler {
	return NewHandler(auth.Service{}, listing.Service{}, favorite.Service{}, ratelimit.DefaultLimits())
}

func TestScalarDocsServesHTML(t *testing.T) {
	h := scalarTestHandler()

	for _, path := range []string{"/docs", "/scalar"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected status 200, got %d", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("%s: expected text/html content type, got %q", path, ct)
		}
		if !strings.Contains(rec.Body.String(), "/openapi.json") {
			t.Fatalf("%s: page does not reference the OpenAPI spec", path)
		}
	}
}

func TestOpenAPISpecIsValidJSON(t *testing.T) {
	h := scalarTestHandler()

	for _, path := range []string{"/openapi.json", "/swagger/doc.json"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected status 200, got %d", path, rec.Code)
		}
		var spec map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
			t.Fatalf("%s: invalid JSON: %v", path, err)
		}
		if _, ok := spec["paths"]; !ok {
			t.Fatalf("%s: spec has no paths", path)
		}
	}
}

func TestOpenAPISpecHostFollowsRequest(t *testing.T) {
	h := scalarTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	req.Host = "eazyrent-production-23a4.up.railway.app"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)

	var spec map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if spec["host"] != "eazyrent-production-23a4.up.railway.app" {
		t.Fatalf("host: got %v", spec["host"])
	}
	schemes, ok := spec["schemes"].([]any)
	if !ok || len(schemes) != 1 || schemes[0] != "https" {
		t.Fatalf("schemes: got %v", spec["schemes"])
	}
}

func TestLegacySwaggerPathRedirectsToDocs(t *testing.T) {
	h := scalarTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/swagger/", nil)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("expected status 301, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/docs" {
		t.Fatalf("expected redirect to /docs, got %q", loc)
	}
}

func TestRewriteSpecHostBadInputReturnsRaw(t *testing.T) {
	raw := []byte("{not json")
	if out := string(rewriteSpecHost(raw, "example.com", "https")); out != string(raw) {
		t.Fatalf("expected raw spec back, got %q", out)
	}
}
