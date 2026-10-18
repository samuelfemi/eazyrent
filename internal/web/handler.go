package web

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/femi/golang-easyrent/internal/auth"
	"github.com/femi/golang-easyrent/internal/favorite"
	"github.com/femi/golang-easyrent/internal/listing"
	"github.com/femi/golang-easyrent/internal/ratelimit"
)

// maxBodyBytes caps request bodies so a client cannot stream unbounded JSON
// into memory. 1 MiB is far above any real payload this API accepts.
const maxBodyBytes = 1 << 20

type Handler struct {
	AuthSvc   auth.Service
	Auth      Auth
	Listings  listing.Service
	Favorites favorite.Service
	Limits    ratelimit.Limits
	// ClientIP derives the rate-limit key for public routes. It defaults to
	// the direct peer (RemoteAddrIP); behind a trusted proxy set it to
	// ForwardedIP so limits key on the real client, not the proxy.
	ClientIP func(*http.Request) string
}

func NewHandler(authSvc auth.Service, listings listing.Service, favorites favorite.Service, limits ratelimit.Limits) Handler {
	return Handler{
		AuthSvc:   authSvc,
		Auth:      Auth{Users: authSvc.Users, Tokens: authSvc.Tokens},
		Listings:  listings,
		Favorites: favorites,
		Limits:    limits,
		ClientIP:  RemoteAddrIP,
	}
}

func (h Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("POST /auth/signup", limit(h.Limits.SignUp, h.ip, 60, h.signUp))
	mux.HandleFunc("POST /auth/signin", limit(h.Limits.SignIn, h.ip, 60, h.signIn))
	mux.HandleFunc("POST /auth/refresh", h.refresh)
	mux.HandleFunc("POST /auth/signout", h.signOut)
	mux.HandleFunc("GET /auth/verify", h.verifyEmail)
	mux.HandleFunc("POST /auth/forgot-password", limit(h.Limits.Reset, h.ip, 60, h.forgotPassword))
	mux.HandleFunc("POST /auth/reset-password", limit(h.Limits.Reset, h.ip, 60, h.resetPassword))
	mux.HandleFunc("GET /me", h.Auth.RequireAuth(h.me))
	mux.HandleFunc("PUT /me/avatar", h.Auth.RequireVerified(h.updateAvatar))
	mux.HandleFunc("GET /listings", limit(h.Limits.List, h.ip, 60, h.listListings))
	mux.HandleFunc("GET /listings/my", h.Auth.RequireVerified(h.myListings))
	mux.HandleFunc("GET /listings/{id}", limit(h.Limits.Detail, h.ip, 60, h.getListing))
	mux.HandleFunc("POST /listings", h.Auth.RequireVerified(limit(h.Limits.Create, userKey("listing-create:"), 3600, h.createListing)))
	mux.HandleFunc("PATCH /listings/{id}", h.Auth.RequireVerified(h.updateListing))
	mux.HandleFunc("DELETE /listings/{id}", h.Auth.RequireVerified(h.deleteListing))
	mux.HandleFunc("PATCH /listings/{id}/status", h.Auth.RequireVerified(h.updateListingStatus))
	mux.HandleFunc("POST /listings/{id}/media", h.Auth.RequireVerified(h.addMedia))
	mux.HandleFunc("POST /favorites/{id}", h.Auth.RequireVerified(limit(h.Limits.FavWrite, userKey("fav-write:"), 60, h.addFavorite)))
	mux.HandleFunc("DELETE /favorites/{id}", h.Auth.RequireVerified(limit(h.Limits.FavWrite, userKey("fav-write:"), 60, h.removeFavorite)))
	mux.HandleFunc("GET /favorites", h.Auth.RequireVerified(limit(h.Limits.FavWrite, userKey("fav-list:"), 60, h.listFavorites)))
	// Scalar API reference + OpenAPI spec. /swagger/* routes are kept as
	// backwards-compatible redirects/aliases for the old Swagger UI.
	mux.HandleFunc("GET /docs", h.scalarDocs)
	mux.HandleFunc("GET /scalar", h.scalarDocs)
	mux.HandleFunc("GET /openapi.json", h.openapiSpecHandler)
	mux.HandleFunc("GET /swagger/doc.json", h.openapiSpecHandler)
	mux.HandleFunc("GET /swagger/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs", http.StatusMovedPermanently)
	})
	return withCORS(withRecover(mux))
}

func (h Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ip derives the caller key for rate limiting, falling back to the peer
// address when ClientIP was not set.
func (h Handler) ip(r *http.Request) string {
	if h.ClientIP != nil {
		return h.ClientIP(r)
	}
	return RemoteAddrIP(r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// decodeJSON reads one JSON value from the request body, capping the size so
// an unbounded body cannot exhaust memory. On failure it has already written
// the response, so callers just return.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

// withRecover turns a handler panic into a logged 500 instead of a dropped
// connection, so clients always get a JSON error.
func withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("web: panic serving %s %s: %v", r.Method, r.URL.Path, rec)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
