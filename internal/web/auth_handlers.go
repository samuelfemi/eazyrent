package web

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/femi/golang-easyrent/internal/auth"
	"github.com/femi/golang-easyrent/internal/listing"
)

type errorResponse struct {
	Error string `json:"error"`
}

func errorStatus(err error) int {
	switch {
	case errors.Is(err, auth.ErrEmailTaken):
		return http.StatusConflict
	case errors.Is(err, auth.ErrInvalidCredentials),
		errors.Is(err, auth.ErrInvalidToken),
		errors.Is(err, auth.ErrTokenExpired):
		return http.StatusUnauthorized
	case errors.Is(err, auth.ErrEmailNotVerified),
		errors.Is(err, listing.ErrListingForbidden):
		return http.StatusForbidden
	case errors.Is(err, auth.ErrInvalidVerificationToken),
		errors.Is(err, auth.ErrInvalidResetToken):
		return http.StatusBadRequest
	case errors.Is(err, listing.ErrListingNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	status := errorStatus(err)
	if status == http.StatusInternalServerError {
		// Logged server-side only; the client keeps the generic message.
		log.Printf("web: internal error %s %s: %v", r.Method, r.URL.Path, err)
		writeError(w, status, "internal error")
		return
	}
	writeError(w, status, err.Error())
}

type signUpRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Phone    string `json:"phone"`
	FullName string `json:"full_name"`
}

func (r signUpRequest) validate() error {
	if _, err := mail.ParseAddress(r.Email); err != nil {
		return errors.New("valid email is required")
	}
	if len(r.Password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	if strings.TrimSpace(r.Phone) == "" {
		return errors.New("phone is required")
	}
	if strings.TrimSpace(r.FullName) == "" {
		return errors.New("full_name is required")
	}
	return nil
}

// signUp godoc
//
//	@Summary		Sign up
//	@Description	Register a user and return the first token pair.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		signUpRequest	true	"Sign up"
//	@Success		201		{object}	auth.AuthTokens
//	@Failure		400		{object}	errorResponse
//	@Failure		409		{object}	errorResponse
//	@Router			/auth/signup [post]
func (h Handler) signUp(w http.ResponseWriter, r *http.Request) {
	var req signUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	req.Phone = strings.TrimSpace(req.Phone)
	req.FullName = strings.TrimSpace(req.FullName)
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tokens, err := h.AuthSvc.SignUp(r.Context(), req.Email, req.Password, req.Phone, req.FullName)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, tokens)
}

type signInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// signIn godoc
//
//	@Summary		Sign in
//	@Description	Sign in with email and password.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		signInRequest	true	"Sign in"
//	@Success		200		{object}	auth.AuthTokens
//	@Failure		400		{object}	errorResponse
//	@Failure		401		{object}	errorResponse
//	@Failure		403		{object}	errorResponse
//	@Router			/auth/signin [post]
func (h Handler) signIn(w http.ResponseWriter, r *http.Request) {
	var req signInRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	tokens, err := h.AuthSvc.SignIn(r.Context(), req.Email, req.Password)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// refresh godoc
//
//	@Summary		Refresh tokens
//	@Description	Rotate a refresh token for a new pair.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		refreshRequest	true	"Refresh"
//	@Success		200		{object}	auth.AuthTokens
//	@Failure		400		{object}	errorResponse
//	@Failure		401		{object}	errorResponse
//	@Router			/auth/refresh [post]
func (h Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.RefreshToken) == "" {
		writeError(w, http.StatusBadRequest, "refresh_token is required")
		return
	}

	tokens, err := h.AuthSvc.Refresh(r.Context(), strings.TrimSpace(req.RefreshToken))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

// signOut godoc
//
//	@Summary		Sign out
//	@Description	Revoke one refresh token. Idempotent.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body	refreshRequest	true	"Sign out"
//	@Success		204
//	@Failure		400	{object}	errorResponse
//	@Router			/auth/signout [post]
func (h Handler) signOut(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.RefreshToken) == "" {
		writeError(w, http.StatusBadRequest, "refresh_token is required")
		return
	}

	if err := h.AuthSvc.SignOut(r.Context(), strings.TrimSpace(req.RefreshToken)); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// verifyEmail godoc
//
//	@Summary		Verify email
//	@Description	Consume an email-verification token.
//	@Tags			auth
//	@Produce		json
//	@Param			token	query		string	true	"Verification token"
//	@Success		200		{object}	map[string]string
//	@Failure		400		{object}	errorResponse
//	@Failure		401		{object}	errorResponse
//	@Router			/auth/verify [get]
func (h Handler) verifyEmail(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		writeError(w, http.StatusBadRequest, "token query param is required")
		return
	}

	if err := h.AuthSvc.VerifyEmail(r.Context(), token); err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "verified"})
}

// userResponse is the public shape of a user: plain JSON-friendly fields
// (also what Swagger documents) instead of the store's sql.Null* types.
type userResponse struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	Phone         string    `json:"phone"`
	FullName      string    `json:"full_name"`
	AvatarURL     *string   `json:"avatar_url"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func toUserResponse(u auth.User) userResponse {
	var avatar *string
	if u.AvatarURL.Valid {
		avatar = &u.AvatarURL.String
	}
	return userResponse{
		ID:            u.ID.String(),
		Email:         u.Email,
		Phone:         u.Phone,
		FullName:      u.FullName,
		AvatarURL:     avatar,
		EmailVerified: u.EmailVerified,
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
	}
}

// me godoc
//
//	@Summary		Current user
//	@Description	Return the caller behind the bearer token.
//	@Tags			auth
//	@Produce		json
//	@Security		Bearer
//	@Success		200	{object}	userResponse
//	@Failure		401	{object}	errorResponse
//	@Router			/me [get]
func (h Handler) me(w http.ResponseWriter, r *http.Request) {
	cu, ok := CurrentUserOf(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}

	user, err := h.AuthSvc.Users.FindByID(r.Context(), cu.UserID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(user))
}

type avatarRequest struct {
	// AvatarURL is the direct image URL returned by the upload provider
	// (e.g. Cloudinary). Empty string clears the avatar.
	AvatarURL string `json:"avatar_url"`
}

// updateAvatar godoc
//
//	@Summary		Set avatar
//	@Description	Store the avatar image URL from the upload provider. Empty string clears it.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			body	body		avatarRequest	true	"Avatar"
//	@Success		200		{object}	userResponse
//	@Failure		400		{object}	errorResponse
//	@Failure		401		{object}	errorResponse
//	@Router			/me/avatar [put]
func (h Handler) updateAvatar(w http.ResponseWriter, r *http.Request) {
	cu, ok := CurrentUserOf(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}

	var req avatarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.AvatarURL = strings.TrimSpace(req.AvatarURL)
	if req.AvatarURL != "" {
		u, err := url.ParseRequestURI(req.AvatarURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			writeError(w, http.StatusBadRequest, "avatar_url must be a valid http(s) URL")
			return
		}
	}

	if err := h.AuthSvc.Users.UpdateAvatar(r.Context(), cu.UserID, req.AvatarURL); err != nil {
		writeServiceError(w, r, err)
		return
	}

	user, err := h.AuthSvc.Users.FindByID(r.Context(), cu.UserID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(user))
}

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

// forgotPassword godoc
//
//	@Summary		Forgot password
//	@Description	Request a password-reset mail. Always succeeds so callers cannot probe for accounts.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		forgotPasswordRequest	true	"Forgot password"
//	@Success		202		{object}	map[string]string
//	@Failure		400		{object}	errorResponse
//	@Router			/auth/forgot-password [post]
func (h Handler) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	if err := h.AuthSvc.RequestPasswordReset(r.Context(), req.Email); err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "if the email is registered, a reset link was sent"})
}

type resetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// resetPassword godoc
//
//	@Summary		Reset password
//	@Description	Consume a reset token and set a new password. Single-use; kills all sessions.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		resetPasswordRequest	true	"Reset password"
//	@Success		200		{object}	map[string]string
//	@Failure		400		{object}	errorResponse
//	@Failure		401		{object}	errorResponse
//	@Router			/auth/reset-password [post]
func (h Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	if len(req.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "new_password must be at least 8 characters")
		return
	}

	if err := h.AuthSvc.ResetPassword(r.Context(), req.Token, req.NewPassword); err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "password reset"})
}
