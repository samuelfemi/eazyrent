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

func TestRemoteAddrIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.7:54321"
	if got := RemoteAddrIP(req); got != "203.0.113.7" {
		t.Fatalf("RemoteAddrIP: got %q", got)
	}

	req.RemoteAddr = "not-a-host-port"
	if got := RemoteAddrIP(req); got != "not-a-host-port" {
		t.Fatalf("RemoteAddrIP without port: got %q", got)
	}
}

func TestForwardedIP(t *testing.T) {
	tests := []struct {
		name    string
		xff     string
		realIP  string
		remote  string
		want    string
	}{
		{"xff leftmost", "198.51.100.9, 10.0.0.1", "", "10.0.0.1:443", "198.51.100.9"},
		{"x-real-ip", "", "198.51.100.22", "10.0.0.1:443", "198.51.100.22"},
		{"fallback to peer", "", "", "10.0.0.1:443", "10.0.0.1"},
		{"blank xff falls back", "  ", "198.51.100.30", "10.0.0.1:443", "198.51.100.30"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remote
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.realIP != "" {
				req.Header.Set("X-Real-IP", tt.realIP)
			}
			if got := ForwardedIP(req); got != tt.want {
				t.Fatalf("ForwardedIP: got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDecodeJSONBodyTooLarge proves an oversized body is rejected with 413
// before any handler logic runs.
func TestDecodeJSONBodyTooLarge(t *testing.T) {
	h := NewHandler(auth.Service{}, listing.Service{}, favorite.Service{}, ratelimit.DefaultLimits())

	big := `{"email":"` + strings.Repeat("a", maxBodyBytes+1) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/signin", strings.NewReader(big))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: want 413, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// TestRecoverMiddleware proves a panicking handler yields a JSON 500 rather
// than a dropped connection.
func TestRecoverMiddleware(t *testing.T) {
	panicking := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	withRecover(panicking).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic: want 500, got %d", rec.Code)
	}
	var body errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error != "internal error" {
		t.Fatalf("panic body: got %q", body.Error)
	}
}
