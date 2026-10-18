package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/femi/golang-easyrent/internal/auth"
	"github.com/google/uuid"
)

type CurrentUser struct {
	UserID        uuid.UUID
	Email         string
	EmailVerified bool
	// User is the fully loaded row, so handlers that already need the user
	// do not have to hit the database a second time.
	User auth.User
}

type currentUserKey struct{}

type Auth struct {
	Users  auth.Store
	Tokens auth.Tokens
}

func (a Auth) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		token = strings.TrimSpace(token)
		if !ok || token == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}

		payload, err := a.Tokens.VerifyAccessToken(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}

		user, err := a.Users.FindByID(r.Context(), payload.UserID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "user not found")
			return
		}

		// A password reset or reuse-detection lockdown bumps this watermark;
		// tokens minted before it are refused even though the JWT is valid.
		if user.TokensValidAfter.Valid && payload.IssuedAt.Before(user.TokensValidAfter.Time) {
			writeError(w, http.StatusUnauthorized, "session no longer valid")
			return
		}

		cu := CurrentUser{UserID: user.ID, Email: user.Email, EmailVerified: user.EmailVerified, User: user}
		ctx := context.WithValue(r.Context(), currentUserKey{}, cu)
		next(w, r.WithContext(ctx))
	}
}

func (a Auth) RequireVerified(next http.HandlerFunc) http.HandlerFunc {
	return a.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		cu, ok := CurrentUserOf(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		if !cu.EmailVerified {
			writeError(w, http.StatusForbidden, "email not verified — please verify your email to continue")
			return
		}
		next(w, r)
	})
}

func CurrentUserOf(r *http.Request) (CurrentUser, bool) {
	u, ok := r.Context().Value(currentUserKey{}).(CurrentUser)
	return u, ok
}

// IsVerifiedBearer reports whether the request carries a valid access token
// for a verified user. Public routes that optionally reveal more to signed-in
// callers use this instead of forcing RequireAuth.
func (a Auth) IsVerifiedBearer(r *http.Request) bool {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	token = strings.TrimSpace(token)
	if !ok || token == "" {
		return false
	}
	payload, err := a.Tokens.VerifyAccessToken(token)
	if err != nil {
		return false
	}
	user, err := a.Users.FindByID(r.Context(), payload.UserID)
	if err != nil {
		return false
	}
	if user.TokensValidAfter.Valid && payload.IssuedAt.Before(user.TokensValidAfter.Time) {
		return false
	}
	return user.EmailVerified
}
