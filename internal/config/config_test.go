package config

import (
	"testing"
	"time"
)

func TestLoadAuth(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/test?sslmode=disable")
	t.Setenv("ACCESS_TOKEN_SECRET", "test-secret")
	t.Setenv("ACCESS_TOKEN_TTL_SECONDS", "900")
	t.Setenv("REFRESH_TOKEN_TTL_DAYS", "7")
	t.Setenv("BREVO_API_KEY", "xkeysib_test")
	t.Setenv("BREVO_API", "")
	t.Setenv("EMAIL_FROM", "EasyRent <test@example.com>")
	t.Setenv("APP_URL", "http://localhost:8080")
	t.Setenv("FRONTEND_URL", "http://localhost:3000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AccessTokenTTL != 15*time.Minute {
		t.Fatalf("AccessTokenTTL: got %v", cfg.AccessTokenTTL)
	}
	if cfg.RefreshTokenTTL != 7*24*time.Hour {
		t.Fatalf("RefreshTokenTTL: got %v", cfg.RefreshTokenTTL)
	}
	if cfg.BrevoAPIKey != "xkeysib_test" || cfg.EmailFrom != "EasyRent <test@example.com>" || cfg.AppURL != "http://localhost:8080" {
		t.Fatalf("mail config: %+v", cfg)
	}
	if cfg.FrontendURL != "http://localhost:3000" {
		t.Fatalf("FrontendURL: got %q", cfg.FrontendURL)
	}
}

func TestLoadFrontendFallback(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/test?sslmode=disable")
	t.Setenv("ACCESS_TOKEN_SECRET", "test-secret")
	t.Setenv("BREVO_API_KEY", "xkeysib_test")
	t.Setenv("BREVO_API", "")
	t.Setenv("APP_URL", "http://localhost:8080")
	t.Setenv("FRONTEND_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.FrontendURL != "http://localhost:8080" {
		t.Fatalf("FrontendURL fallback: got %q", cfg.FrontendURL)
	}
}

func TestLoadBrevoFallbackKey(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/test?sslmode=disable")
	t.Setenv("ACCESS_TOKEN_SECRET", "test-secret")
	t.Setenv("BREVO_API_KEY", "")
	t.Setenv("BREVO_API", "xkeysib_fallback")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BrevoAPIKey != "xkeysib_fallback" {
		t.Fatalf("BrevoAPIKey fallback: got %q", cfg.BrevoAPIKey)
	}
}

func TestLoadMissingSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/test?sslmode=disable")
	t.Setenv("ACCESS_TOKEN_SECRET", "")
	t.Setenv("BREVO_API_KEY", "xkeysib_test")
	t.Setenv("BREVO_API", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load should fail without ACCESS_TOKEN_SECRET")
	}
}

func TestLoadMissingDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("ACCESS_TOKEN_SECRET", "test-secret")
	t.Setenv("BREVO_API_KEY", "xkeysib_test")
	t.Setenv("BREVO_API", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load should fail without DATABASE_URL")
	}
}

func TestLoadBadInt(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/test?sslmode=disable")
	t.Setenv("ACCESS_TOKEN_SECRET", "test-secret")
	t.Setenv("ACCESS_TOKEN_TTL_SECONDS", "not-a-number")
	t.Setenv("BREVO_API_KEY", "xkeysib_test")
	t.Setenv("BREVO_API", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load should fail on non-integer ACCESS_TOKEN_TTL_SECONDS")
	}
}

func TestLoadMissingBrevoKey(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/test?sslmode=disable")
	t.Setenv("ACCESS_TOKEN_SECRET", "test-secret")
	t.Setenv("BREVO_API_KEY", "")
	t.Setenv("BREVO_API", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load should fail without BREVO_API_KEY")
	}
}
