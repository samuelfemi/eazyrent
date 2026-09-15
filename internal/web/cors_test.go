package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/femi/golang-easyrent/internal/auth"
	"github.com/femi/golang-easyrent/internal/favorite"
	"github.com/femi/golang-easyrent/internal/listing"
	"github.com/femi/golang-easyrent/internal/ratelimit"
)

func TestCORSPreflight(t *testing.T) {
	h := NewHandler(auth.Service{}, listing.Service{}, favorite.Service{}, ratelimit.DefaultLimits())

	req := httptest.NewRequest(http.MethodOptions, "/auth/signin", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for preflight, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("missing Access-Control-Allow-Origin on preflight")
	}
}

func TestCORSHeaderOnHealth(t *testing.T) {
	h := NewHandler(auth.Service{}, listing.Service{}, favorite.Service{}, ratelimit.DefaultLimits())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("missing Access-Control-Allow-Origin on GET")
	}
}
