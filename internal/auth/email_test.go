package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerificationHTML(t *testing.T) {
	html := verificationHTML("Ada Lovelace", "http://localhost:8080/auth/verify?token=abc123")
	mustContain := []string{"EASYRENT", "Confirm your email", "Ada Lovelace", "Verify email", "abc123", "24 hours", "#2563eb", "If the button doesn't work"}
	for _, s := range mustContain {
		if !strings.Contains(html, s) {
			t.Fatalf("verification html missing %q", s)
		}
	}
	if strings.Contains(html, "<script") {
		t.Fatal("html should not contain script")
	}
}

func TestVerificationHTMLEscaping(t *testing.T) {
	html := verificationHTML("<b>evil</b>", "http://localhost:8080/auth/verify?token=x")
	if strings.Contains(html, "<b>evil</b>") {
		t.Fatal("name not escaped")
	}
	if !strings.Contains(html, "&lt;b&gt;evil&lt;/b&gt;") {
		t.Fatal("escaped name missing")
	}
}

func TestResetHTML(t *testing.T) {
	html := resetHTML("Femi", "http://localhost:8080/reset-password?token=xyz")
	mustContain := []string{"Reset your password", "Femi", "Reset password", "xyz", "1 hour", "signed out"}
	for _, s := range mustContain {
		if !strings.Contains(html, s) {
			t.Fatalf("reset html missing %q", s)
		}
	}
}

func TestLinkEscaping(t *testing.T) {
	token := "a+b/c?d=e&f"
	link := verificationLink("http://localhost:8080", token)
	if !strings.Contains(link, "a%2Bb%2Fc%3Fd%3De%26f") {
		t.Fatalf("token not query-escaped: %q", link)
	}
}

func TestEmptyNameFallback(t *testing.T) {
	html := verificationHTML("", "http://localhost:8080/auth/verify?token=x")
	if !strings.Contains(html, "Hi there") {
		t.Fatalf("empty name should fallback to there, got %q", html)
	}
}

func TestParseSender(t *testing.T) {
	name, email := parseSender("EasyRent <noreply@example.com>")
	if name != "EasyRent" || email != "noreply@example.com" {
		t.Fatalf("parseSender named: got %q %q", name, email)
	}
	name, email = parseSender("noreply@example.com")
	if name != "" || email != "noreply@example.com" {
		t.Fatalf("parseSender bare: got %q %q", name, email)
	}
	if _, email := parseSender(""); email != "" {
		t.Fatalf("parseSender empty: got %q", email)
	}
}

func TestSendViaBrevo(t *testing.T) {
	var gotPath, gotAPIKey, gotContentType string
	var gotPayload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("api-key")
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"messageId":"<test-id@brevo>"}`))
	}))
	defer srv.Close()

	s := EmailSender{APIKey: "xkeysib_test", From: "EasyRent <noreply@example.com>", AppURL: "http://localhost:8080", BaseURL: srv.URL, HTTPClient: srv.Client()}
	if err := s.SendVerificationEmail("user@example.com", "Ada", "tok123"); err != nil {
		t.Fatalf("SendVerificationEmail: %v", err)
	}

	if gotPath != "/smtp/email" {
		t.Fatalf("path: got %q", gotPath)
	}
	if gotAPIKey != "xkeysib_test" {
		t.Fatalf("api-key header: got %q", gotAPIKey)
	}
	if !strings.Contains(gotContentType, "application/json") {
		t.Fatalf("content-type: got %q", gotContentType)
	}
	sender, _ := gotPayload["sender"].(map[string]any)
	if sender["email"] != "noreply@example.com" || sender["name"] != "EasyRent" {
		t.Fatalf("sender payload: %v", sender)
	}
	to, _ := gotPayload["to"].([]any)
	if len(to) != 1 {
		t.Fatalf("to payload: %v", gotPayload["to"])
	}
	if subj, _ := gotPayload["subject"].(string); !strings.Contains(subj, "Confirm") {
		t.Fatalf("subject payload: %q", subj)
	}
	if html, _ := gotPayload["htmlContent"].(string); !strings.Contains(html, "tok123") {
		t.Fatal("htmlContent missing token")
	}
}

func TestSendViaBrevoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Key not found"}`))
	}))
	defer srv.Close()

	s := EmailSender{APIKey: "bad", From: "EasyRent <noreply@example.com>", BaseURL: srv.URL, HTTPClient: srv.Client()}
	if err := s.send("user@example.com", "hi", "<p>hi</p>"); err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestSendInvalidFrom(t *testing.T) {
	s := EmailSender{APIKey: "x", From: "  "}
	if err := s.send("user@example.com", "hi", "<p>hi</p>"); err == nil {
		t.Fatal("expected error on empty EMAIL_FROM")
	}
}
