package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/femi/golang-easyrent/internal/auth"
	"github.com/femi/golang-easyrent/internal/favorite"
	"github.com/femi/golang-easyrent/internal/listing"
	"github.com/femi/golang-easyrent/internal/ratelimit"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// testHandler builds a Handler wired to the dev database, skipping when
// DATABASE_URL is unset. Callers clean up the rows they create.
func testHandler(t *testing.T) (Handler, *sql.DB) {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set, skipping DB test")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	svc := auth.NewService(
		auth.NewStore(db),
		auth.Tokens{Secret: []byte("handler-test-secret-please-ignore"), AccessTTL: time.Hour},
		auth.EmailSender{},
		30*24*time.Hour,
	)
	return NewHandler(svc, listing.NewService(listing.NewStore(db)), favorite.NewService(favorite.NewStore(db)), ratelimit.DefaultLimits()), db
}

func doJSON(t *testing.T, mux http.Handler, method, target, body, authHeader string) *httptest.ResponseRecorder {
	t.Helper()

	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(v); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}

// TestAuthEndpoints runs the auth flow at HTTP level: status codes, error
// mapping and token rotation, end to end through the mux.
func TestAuthEndpoints(t *testing.T) {
	h, db := testHandler(t)
	mux := h.Routes()

	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("http-test-%d@example.com", suffix)
	phone := fmt.Sprintf("+234%013d", suffix%10000000000000)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE email = $1`, email)
	})

	signupBody := fmt.Sprintf(`{"email":%q,"password":"correct-horse-123","phone":%q,"full_name":"HTTP User"}`, email, phone)
	rec := doJSON(t, mux, http.MethodPost, "/auth/signup", signupBody, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var pair auth.AuthTokens
	decodeBody(t, rec, &pair)
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("signup should return both tokens")
	}

	// Invalid payloads and duplicates.
	rec = doJSON(t, mux, http.MethodPost, "/auth/signup", `{"email":"bad","password":"x","phone":"","full_name":""}`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad signup: want 400, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodPost, "/auth/signup", signupBody, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate signup: want 409, got %d (%s)", rec.Code, rec.Body.String())
	}

	signinBody := fmt.Sprintf(`{"email":%q,"password":"wrong-password"}`, email)
	rec = doJSON(t, mux, http.MethodPost, "/auth/signin", signinBody, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: want 401, got %d", rec.Code)
	}
	signinBody = fmt.Sprintf(`{"email":%q,"password":"correct-horse-123"}`, email)
	rec = doJSON(t, mux, http.MethodPost, "/auth/signin", signinBody, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unverified signin: want 403, got %d", rec.Code)
	}

	// Verification: bogus token, then the real one from the store.
	rec = doJSON(t, mux, http.MethodGet, "/auth/verify?token=bogus", "", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus verify: want 400, got %d", rec.Code)
	}
	var stored auth.User
	if err := db.QueryRow(`SELECT verification_token FROM users WHERE email = $1`, email).Scan(&stored.VerificationToken); err != nil {
		t.Fatalf("read verification token: %v", err)
	}
	if !stored.VerificationToken.Valid {
		t.Fatal("new user should hold a verification token")
	}
	rec = doJSON(t, mux, http.MethodGet, "/auth/verify?token="+stored.VerificationToken.String, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, mux, http.MethodPost, "/auth/signin", signinBody, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("signin: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	decodeBody(t, rec, &pair)

	// Protected endpoint.
	rec = doJSON(t, mux, http.MethodGet, "/me", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous /me: want 401, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodGet, "/me", "", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("/me: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var me userResponse
	decodeBody(t, rec, &me)
	if me.Email != email {
		t.Fatalf("wrong /me body: %+v", me)
	}

	// Refresh rotation and reuse detection.
	refreshBody := fmt.Sprintf(`{"refresh_token":%q}`, pair.RefreshToken)
	rec = doJSON(t, mux, http.MethodPost, "/auth/refresh", refreshBody, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var rotated auth.AuthTokens
	decodeBody(t, rec, &rotated)
	rec = doJSON(t, mux, http.MethodPost, "/auth/refresh", refreshBody, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh reuse: want 401, got %d", rec.Code)
	}

	// Sign-out: unknown token is 204, real token is 204 and kills the token.
	rec = doJSON(t, mux, http.MethodPost, "/auth/signout", `{"refresh_token":"unknown"}`, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("signout unknown: want 204, got %d", rec.Code)
	}

	rec = doJSON(t, mux, http.MethodPost, "/auth/signin", signinBody, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("signin for signout: want 200, got %d", rec.Code)
	}
	decodeBody(t, rec, &pair)
	rec = doJSON(t, mux, http.MethodPost, "/auth/signout", fmt.Sprintf(`{"refresh_token":%q}`, pair.RefreshToken), "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("signout: want 204, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, mux, http.MethodPost, "/auth/refresh", fmt.Sprintf(`{"refresh_token":%q}`, pair.RefreshToken), "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after signout: want 401, got %d", rec.Code)
	}
}

// TestPasswordResetEndpoints covers forgot/reset at HTTP level, including
// the silent-unknown-email rule and single-use tokens.
func TestPasswordResetEndpoints(t *testing.T) {
	h, db := testHandler(t)
	mux := h.Routes()

	// Unknown email still succeeds: no account probing via status codes.
	rec := doJSON(t, mux, http.MethodPost, "/auth/forgot-password", `{"email":"nobody@example.com"}`, "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("forgot unknown: want 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, mux, http.MethodPost, "/auth/forgot-password", `{"email":""}`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("forgot empty: want 400, got %d", rec.Code)
	}

	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("http-reset-%d@example.com", suffix)
	phone := fmt.Sprintf("+234%013d", suffix%10000000000000)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE email = $1`, email)
	})

	signupBody := fmt.Sprintf(`{"email":%q,"password":"old-password-123","phone":%q,"full_name":"HTTP Reset"}`, email, phone)
	if rec := doJSON(t, mux, http.MethodPost, "/auth/signup", signupBody, ""); rec.Code != http.StatusCreated {
		t.Fatalf("signup: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, mux, http.MethodPost, "/auth/forgot-password", fmt.Sprintf(`{"email":%q}`, email), "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("forgot: want 202, got %d (%s)", rec.Code, rec.Body.String())
	}

	var userID string
	if err := db.QueryRowContext(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID); err != nil {
		t.Fatalf("find user id: %v", err)
	}

	// Known raw token inserted directly; only its hash reaches the database.
	const rawToken = "http-reset-raw-token"
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO password_reset_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, now() + interval '1 hour')`,
		userID, auth.HashRefreshToken(rawToken)); err != nil {
		t.Fatalf("insert reset token: %v", err)
	}

	rec = doJSON(t, mux, http.MethodPost, "/auth/reset-password", `{"token":"bogus","new_password":"new-password-123"}`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reset bogus: want 400, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodPost, "/auth/reset-password", fmt.Sprintf(`{"token":%q,"new_password":"short"}`, rawToken), "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reset short password: want 400, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodPost, "/auth/reset-password", fmt.Sprintf(`{"token":%q,"new_password":"new-password-123"}`, rawToken), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, mux, http.MethodPost, "/auth/reset-password", fmt.Sprintf(`{"token":%q,"new_password":"new-password-123"}`, rawToken), "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reset reuse: want 400, got %d", rec.Code)
	}

	// New password works only after verification, like any sign-in.
	var verifyToken sql.NullString
	if err := db.QueryRowContext(context.Background(), `SELECT verification_token FROM users WHERE email = $1`, email).Scan(&verifyToken); err != nil {
		t.Fatalf("read verification token: %v", err)
	}
	if rec := doJSON(t, mux, http.MethodGet, "/auth/verify?token="+verifyToken.String, "", ""); rec.Code != http.StatusOK {
		t.Fatalf("verify: want 200, got %d", rec.Code)
	}
	signin := fmt.Sprintf(`{"email":%q,"password":"new-password-123"}`, email)
	if rec := doJSON(t, mux, http.MethodPost, "/auth/signin", signin, ""); rec.Code != http.StatusOK {
		t.Fatalf("signin new password: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// TestUpdateAvatar covers the avatar endpoint: auth gate, URL validation,
// persist, and clear.
func TestUpdateAvatar(t *testing.T) {
	h, db := testHandler(t)
	mux := h.Routes()

	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("http-avatar-%d@example.com", suffix)
	phone := fmt.Sprintf("+234%013d", suffix%10000000000000)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE email = $1`, email)
	})

	signupBody := fmt.Sprintf(`{"email":%q,"password":"correct-horse-123","phone":%q,"full_name":"Avatar User"}`, email, phone)
	if rec := doJSON(t, mux, http.MethodPost, "/auth/signup", signupBody, ""); rec.Code != http.StatusCreated {
		t.Fatalf("signup: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var vtoken sql.NullString
	if err := db.QueryRowContext(context.Background(), `SELECT verification_token FROM users WHERE email = $1`, email).Scan(&vtoken); err != nil {
		t.Fatalf("read verification token: %v", err)
	}
	if rec := doJSON(t, mux, http.MethodGet, "/auth/verify?token="+vtoken.String, "", ""); rec.Code != http.StatusOK {
		t.Fatalf("verify: want 200, got %d", rec.Code)
	}
	signinBody := fmt.Sprintf(`{"email":%q,"password":"correct-horse-123"}`, email)
	rec := doJSON(t, mux, http.MethodPost, "/auth/signin", signinBody, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("signin: want 200, got %d", rec.Code)
	}
	var pair auth.AuthTokens
	decodeBody(t, rec, &pair)
	bearer := "Bearer " + pair.AccessToken

	rec = doJSON(t, mux, http.MethodPut, "/me/avatar", `{"avatar_url":"https://res.cloudinary.com/demo/image/upload/v1/sample.jpg"}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous avatar: want 401, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodPut, "/me/avatar", `{"avatar_url":"not-a-url"}`, bearer)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad url: want 400, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodPut, "/me/avatar", `{"avatar_url":"ftp://files/x.jpg"}`, bearer)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-http url: want 400, got %d", rec.Code)
	}

	rec = doJSON(t, mux, http.MethodPut, "/me/avatar", `{"avatar_url":"https://res.cloudinary.com/demo/image/upload/v1/sample.jpg"}`, bearer)
	if rec.Code != http.StatusOK {
		t.Fatalf("set avatar: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var me userResponse
	decodeBody(t, rec, &me)
	if me.AvatarURL == nil || *me.AvatarURL != "https://res.cloudinary.com/demo/image/upload/v1/sample.jpg" {
		t.Fatalf("avatar not persisted: %+v", me)
	}

	rec = doJSON(t, mux, http.MethodPut, "/me/avatar", `{"avatar_url":""}`, bearer)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear avatar: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	decodeBody(t, rec, &me)
	if me.AvatarURL != nil {
		t.Fatalf("avatar not cleared: %+v", me)
	}
}
