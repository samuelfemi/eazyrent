package web

import (
	"encoding/json"
	"net/http"

	"github.com/femi/golang-easyrent/docs"
	"github.com/femi/golang-easyrent/internal/auth"
	"github.com/femi/golang-easyrent/internal/favorite"
	"github.com/femi/golang-easyrent/internal/listing"
	"github.com/femi/golang-easyrent/internal/ratelimit"

	httpSwagger "github.com/swaggo/http-swagger"
)

type Handler struct {
	AuthSvc   auth.Service
	Auth      Auth
	Listings  listing.Service
	Favorites favorite.Service
	Limits    ratelimit.Limits
}

func NewHandler(authSvc auth.Service, listings listing.Service, favorites favorite.Service, limits ratelimit.Limits) Handler {
	return Handler{
		AuthSvc:   authSvc,
		Auth:      Auth{Users: authSvc.Users, Tokens: authSvc.Tokens},
		Listings:  listings,
		Favorites: favorites,
		Limits:    limits,
	}
}

func (h Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("POST /auth/signup", h.signUp)
	mux.HandleFunc("POST /auth/signin", h.signIn)
	mux.HandleFunc("POST /auth/refresh", h.refresh)
	mux.HandleFunc("POST /auth/signout", h.signOut)
	mux.HandleFunc("GET /auth/verify", h.verifyEmail)
	mux.HandleFunc("POST /auth/forgot-password", h.forgotPassword)
	mux.HandleFunc("POST /auth/reset-password", h.resetPassword)
	mux.HandleFunc("GET /me", h.Auth.RequireAuth(h.me))
	mux.HandleFunc("PUT /me/avatar", h.Auth.RequireVerified(h.updateAvatar))
	mux.HandleFunc("GET /listings", limit(h.Limits.List, clientIP, 60, h.listListings))
	mux.HandleFunc("GET /listings/my", h.Auth.RequireVerified(h.myListings))
	mux.HandleFunc("GET /listings/{id}", limit(h.Limits.Detail, clientIP, 60, h.getListing))
	mux.HandleFunc("POST /listings", h.Auth.RequireVerified(limit(h.Limits.Create, userKey("listing-create:"), 3600, h.createListing)))
	mux.HandleFunc("PATCH /listings/{id}", h.Auth.RequireVerified(h.updateListing))
	mux.HandleFunc("DELETE /listings/{id}", h.Auth.RequireVerified(h.deleteListing))
	mux.HandleFunc("PATCH /listings/{id}/status", h.Auth.RequireVerified(h.updateListingStatus))
	mux.HandleFunc("POST /listings/{id}/media", h.Auth.RequireVerified(h.addMedia))
	mux.HandleFunc("POST /favorites/{id}", h.Auth.RequireVerified(limit(h.Limits.FavWrite, userKey("fav-write:"), 60, h.addFavorite)))
	mux.HandleFunc("DELETE /favorites/{id}", h.Auth.RequireVerified(limit(h.Limits.FavWrite, userKey("fav-write:"), 60, h.removeFavorite)))
	mux.HandleFunc("GET /favorites", h.Auth.RequireVerified(limit(h.Limits.FavWrite, userKey("fav-list:"), 60, h.listFavorites)))
	mux.Handle("/swagger/", swaggerHandler())
	return withCORS(mux)
}

// swaggerHandler serves Swagger UI with a per-request host/scheme derived from
// the incoming request (X-Forwarded-Host/Proto when behind Railway). This
// makes "Try it out" work even if APP_URL is not set on the host.
func swaggerHandler() http.Handler {
	base := httpSwagger.WrapHandler
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Header.Get("X-Forwarded-Host")
		if host == "" {
			host = r.Host
		}
		scheme := r.Header.Get("X-Forwarded-Proto")
		if scheme == "" {
			if r.TLS != nil {
				scheme = "https"
			} else {
				scheme = "http"
			}
		}

		origHost, origSchemes := docs.SwaggerInfo.Host, docs.SwaggerInfo.Schemes
		if host != "" {
			docs.SwaggerInfo.Host = host
		}
		if scheme == "https" || scheme == "http" {
			docs.SwaggerInfo.Schemes = []string{scheme}
		}

		base.ServeHTTP(w, r)

		docs.SwaggerInfo.Host, docs.SwaggerInfo.Schemes = origHost, origSchemes
	})
}

func (h Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
