package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// openTestDB connects to the dev database. It skips when DATABASE_URL is
// unset so `go test` still passes without a database.
func openTestDB(t *testing.T) *sql.DB {
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}
	return db
}

func TestAuthStoreLifecycle(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("auth-test-%d@example.com", suffix)
	phone := fmt.Sprintf("+234%013d", suffix%10000000000000)

	u, err := store.CreateUser(ctx, email, phone, "bcrypt-hash-placeholder", "Test User")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, u.ID)
	})

	if u.EmailVerified {
		t.Fatal("new user should not be email-verified")
	}

	byEmail, err := store.FindByEmail(ctx, email)
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if byEmail.ID != u.ID {
		t.Fatalf("FindByEmail returned wrong user: %v != %v", byEmail.ID, u.ID)
	}

	if _, err := store.FindByEmail(ctx, "missing@example.com"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("FindByEmail missing: want ErrUserNotFound, got %v", err)
	}

	byID, err := store.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if byID.Email != email {
		t.Fatalf("FindByID returned wrong email: %q", byID.Email)
	}

	// Email verification flow.
	verifyToken := fmt.Sprintf("verify-%d", suffix)
	verifyExpiry := time.Now().Add(24 * time.Hour).UTC()
	if err := store.StoreVerificationToken(ctx, u.ID, verifyToken, verifyExpiry); err != nil {
		t.Fatalf("StoreVerificationToken: %v", err)
	}

	byToken, err := store.FindByVerificationToken(ctx, verifyToken)
	if err != nil {
		t.Fatalf("FindByVerificationToken: %v", err)
	}
	if byToken.ID != u.ID || !byToken.VerificationTokenExpiresAt.Valid {
		t.Fatal("FindByVerificationToken returned wrong user or missing expiry")
	}

	if err := store.MarkEmailVerified(ctx, u.ID); err != nil {
		t.Fatalf("MarkEmailVerified: %v", err)
	}

	verified, err := store.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("FindByID after verify: %v", err)
	}
	if !verified.EmailVerified {
		t.Fatal("user should be email-verified after MarkEmailVerified")
	}
	if verified.VerificationToken.Valid || verified.VerificationTokenExpiresAt.Valid {
		t.Fatal("verification token should be cleared after MarkEmailVerified")
	}
	if _, err := store.FindByVerificationToken(ctx, verifyToken); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("reused verification token: want ErrUserNotFound, got %v", err)
	}

	// Refresh token flow.
	hash1 := fmt.Sprintf("hash-%d-1", suffix)
	hash2 := fmt.Sprintf("hash-%d-2", suffix)
	expiry := time.Now().Add(30 * 24 * time.Hour).UTC()
	if err := store.StoreRefreshToken(ctx, u.ID, hash1, expiry); err != nil {
		t.Fatalf("StoreRefreshToken 1: %v", err)
	}
	if err := store.StoreRefreshToken(ctx, u.ID, hash2, expiry); err != nil {
		t.Fatalf("StoreRefreshToken 2: %v", err)
	}

	tok1, err := store.FindRefreshToken(ctx, hash1)
	if err != nil {
		t.Fatalf("FindRefreshToken: %v", err)
	}
	if tok1.UserID != u.ID || tok1.RevokedAt.Valid {
		t.Fatal("fresh token should belong to user and not be revoked")
	}
	if _, err := store.FindRefreshToken(ctx, "missing-hash"); !errors.Is(err, ErrRefreshTokenNotFound) {
		t.Fatalf("FindRefreshToken missing: want ErrRefreshTokenNotFound, got %v", err)
	}

	if err := store.RevokeRefreshToken(ctx, tok1.ID); err != nil {
		t.Fatalf("RevokeRefreshToken: %v", err)
	}
	revoked, err := store.FindRefreshToken(ctx, hash1)
	if err != nil {
		t.Fatalf("FindRefreshToken after revoke: %v", err)
	}
	if !revoked.RevokedAt.Valid {
		t.Fatal("token should be revoked after RevokeRefreshToken")
	}

	if err := store.RevokeAllUserTokens(ctx, u.ID); err != nil {
		t.Fatalf("RevokeAllUserTokens: %v", err)
	}
	tok2, err := store.FindRefreshToken(ctx, hash2)
	if err != nil {
		t.Fatalf("FindRefreshToken 2 after revoke-all: %v", err)
	}
	if !tok2.RevokedAt.Valid {
		t.Fatal("second token should be revoked after RevokeAllUserTokens")
	}

	// Revoking all sessions must also fence out already-issued access tokens.
	afterRevoke, err := store.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("FindByID after revoke-all: %v", err)
	}
	if !afterRevoke.TokensValidAfter.Valid {
		t.Fatal("RevokeAllUserTokens should set tokens_valid_after")
	}
}
