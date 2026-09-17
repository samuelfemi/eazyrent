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

		ctx := context.WithValue(r.Context(), currentUserKey{}, CurrentUser{UserID: user.ID, Email: user.Email, EmailVerified: user.EmailVerified})
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

func (a Auth) IsVerifiedRequest(r *http.Request) bool {
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
	return user.EmailVerified
}
