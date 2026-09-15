package web

import (
	"encoding/json"
	"net/http"

	"github.com/femi/golang-easyrent/internal/auth"
	"github.com/femi/golang-easyrent/internal/favorite"
	"github.com/femi/golang-easyrent/internal/listing"
	"github.com/femi/golang-easyrent/internal/ratelimit"

	_ "github.com/femi/golang-easyrent/docs"
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
	mux.HandleFunc("PUT /me/avatar", h.Auth.RequireAuth(h.updateAvatar))
	mux.HandleFunc("GET /listings", limit(h.Limits.List, clientIP, 60, h.listListings))
	mux.HandleFunc("GET /listings/my", h.Auth.RequireAuth(h.myListings))
	mux.HandleFunc("GET /listings/{id}", limit(h.Limits.Detail, clientIP, 60, h.getListing))
	mux.HandleFunc("POST /listings", h.Auth.RequireAuth(limit(h.Limits.Create, userKey("listing-create:"), 3600, h.createListing)))
	mux.HandleFunc("PATCH /listings/{id}", h.Auth.RequireAuth(h.updateListing))
	mux.HandleFunc("DELETE /listings/{id}", h.Auth.RequireAuth(h.deleteListing))
	mux.HandleFunc("PATCH /listings/{id}/status", h.Auth.RequireAuth(h.updateListingStatus))
	mux.HandleFunc("POST /listings/{id}/media", h.Auth.RequireAuth(h.addMedia))
	mux.HandleFunc("POST /favorites/{id}", h.Auth.RequireAuth(limit(h.Limits.FavWrite, userKey("fav-write:"), 60, h.addFavorite)))
	mux.HandleFunc("DELETE /favorites/{id}", h.Auth.RequireAuth(limit(h.Limits.FavWrite, userKey("fav-write:"), 60, h.removeFavorite)))
	mux.HandleFunc("GET /favorites", h.Auth.RequireAuth(limit(h.Limits.FavWrite, userKey("fav-list:"), 60, h.listFavorites)))
	mux.Handle("/swagger/", httpSwagger.WrapHandler)
	return withCORS(mux)
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
