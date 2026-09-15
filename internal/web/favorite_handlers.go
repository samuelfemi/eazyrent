package web

import (
	"net/http"

	"github.com/google/uuid"
)

// pathFavoriteID parses the {id} path value for favorites (a listing id).
func pathFavoriteID(r *http.Request) (uuid.UUID, error) {
	return pathListingID(r)
}

// addFavorite godoc
//
//	@Summary		Add favorite
//	@Description	Save a listing to the caller's favorites. Idempotent.
//	@Tags			favorites
//	@Produce		json
//	@Security		Bearer
//	@Param			id	path		string	true	"Listing ID"
//	@Success		201	{object}	map[string]string
//	@Failure		400	{object}	errorResponse
//	@Failure		401	{object}	errorResponse
//	@Failure		404	{object}	errorResponse
//	@Failure		429	{object}	errorResponse
//	@Router			/favorites/{id} [post]
func (h Handler) addFavorite(w http.ResponseWriter, r *http.Request) {
	cu, ok := CurrentUserOf(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	listingID, err := pathFavoriteID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Favorites.Add(r.Context(), cu.UserID, listingID); err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "favorited"})
}

// removeFavorite godoc
//
//	@Summary		Remove favorite
//	@Description	Remove a listing from the caller's favorites. Idempotent.
//	@Tags			favorites
//	@Security		Bearer
//	@Param			id	path	string	true	"Listing ID"
//	@Success		204
//	@Failure		400	{object}	errorResponse
//	@Failure		401	{object}	errorResponse
//	@Failure		429	{object}	errorResponse
//	@Router			/favorites/{id} [delete]
func (h Handler) removeFavorite(w http.ResponseWriter, r *http.Request) {
	cu, ok := CurrentUserOf(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	listingID, err := pathFavoriteID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Favorites.Remove(r.Context(), cu.UserID, listingID); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listFavorites godoc
//
//	@Summary		List favorites
//	@Description	Paginated favorite listings of the authenticated user, newest favorite first.
//	@Tags			favorites
//	@Produce		json
//	@Security		Bearer
//	@Param			page	query		int	false	"Page"
//	@Param			limit	query		int	false	"Page size (1-100)"
//	@Success		200	{object}	listingPageResponse
//	@Failure		400	{object}	errorResponse
//	@Failure		401	{object}	errorResponse
//	@Failure		429	{object}	errorResponse
//	@Router			/favorites [get]
func (h Handler) listFavorites(w http.ResponseWriter, r *http.Request) {
	cu, ok := CurrentUserOf(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	page, limit, err := parsePageQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := h.Favorites.List(r.Context(), cu.UserID, page, limit)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toPageResponse(p))
}
