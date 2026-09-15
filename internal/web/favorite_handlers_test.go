package web

import (
	"database/sql"
	"net/http"
	"testing"
)

// TestFavoriteEndpoints covers the favorites HTTP layer: auth gates, 404
// on unknown listing, idempotent add/remove, list pagination and that
// FavoriteCount on listings reflects favorites.
func TestFavoriteEndpoints(t *testing.T) {
	h, db := testHandler(t)
	mux := h.Routes()
	t.Cleanup(func() {
		_, _ = db.ExecContext(t.Context(), `DELETE FROM listings WHERE title LIKE 'Fav HTTP%'`)
	})

	_, owner := testLandlordWithToken(t, db, h, "fav-owner")
	_, other := testLandlordWithToken(t, db, h, "fav-other")

	// Need two listings to exercise ordering and counts.
	rec := doJSON(t, mux, http.MethodPost, "/listings", createListingBody("Fav HTTP One"), owner)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create one: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var one listingResponse
	decodeBody(t, rec, &one)

	rec = doJSON(t, mux, http.MethodPost, "/listings", createListingBody("Fav HTTP Two"), owner)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create two: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var two listingResponse
	decodeBody(t, rec, &two)

	// Auth gates.
	if rec := doJSON(t, mux, http.MethodPost, "/favorites/"+one.ID, "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous add: want 401, got %d", rec.Code)
	}
	if rec := doJSON(t, mux, http.MethodGet, "/favorites", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list: want 401, got %d", rec.Code)
	}
	if rec := doJSON(t, mux, http.MethodGet, "/favorites?page=0", "", other); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad page: want 400, got %d", rec.Code)
	}
	if rec := doJSON(t, mux, http.MethodPost, "/favorites/not-a-uuid", "", other); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: want 400, got %d", rec.Code)
	}
	if rec := doJSON(t, mux, http.MethodPost, "/favorites/00000000-0000-0000-0000-000000000000", "", other); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown listing: want 404, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Add, idempotent add, count.
	rec = doJSON(t, mux, http.MethodPost, "/favorites/"+one.ID, "", other)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, mux, http.MethodPost, "/favorites/"+one.ID, "", other)
	if rec.Code != http.StatusCreated {
		t.Fatalf("idempotent add: want 201, got %d", rec.Code)
	}
	// FavoriteCount should now be 1.
	rec = doJSON(t, mux, http.MethodGet, "/listings/"+one.ID, "", "")
	var detail listingDetailResponse
	decodeBody(t, rec, &detail)
	if detail.Listing.FavoriteCount != 1 {
		t.Fatalf("favorite count: want 1, got %d", detail.Listing.FavoriteCount)
	}

	rec = doJSON(t, mux, http.MethodPost, "/favorites/"+two.ID, "", other)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add two: want 201, got %d", rec.Code)
	}

	// List: newest favorite first (two then one), pagination.
	rec = doJSON(t, mux, http.MethodGet, "/favorites?limit=1&page=1", "", other)
	if rec.Code != http.StatusOK {
		t.Fatalf("list p1: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var p1 listingPageResponse
	decodeBody(t, rec, &p1)
	if p1.Total != 2 || len(p1.Data) != 1 || p1.Data[0].ID != two.ID {
		t.Fatalf("p1: %+v", p1)
	}
	rec = doJSON(t, mux, http.MethodGet, "/favorites?limit=1&page=2", "", other)
	var p2 listingPageResponse
	decodeBody(t, rec, &p2)
	if len(p2.Data) != 1 || p2.Data[0].ID != one.ID {
		t.Fatalf("p2: %+v", p2)
	}
	// Other user sees none.
	rec = doJSON(t, mux, http.MethodGet, "/favorites", "", owner)
	var mine listingPageResponse
	decodeBody(t, rec, &mine)
	if mine.Total != 0 {
		t.Fatalf("owner should have no favorites: %+v", mine)
	}

	// Remove, idempotent remove.
	rec = doJSON(t, mux, http.MethodDelete, "/favorites/"+one.ID, "", other)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove: want 204, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodDelete, "/favorites/"+one.ID, "", other)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("idempotent remove: want 204, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodGet, "/listings/"+one.ID, "", "")
	decodeBody(t, rec, &detail)
	if detail.Listing.FavoriteCount != 0 {
		t.Fatalf("count after remove: want 0, got %d", detail.Listing.FavoriteCount)
	}

	// List should now have one left.
	rec = doJSON(t, mux, http.MethodGet, "/favorites", "", other)
	var after listingPageResponse
	decodeBody(t, rec, &after)
	if after.Total != 1 || after.Data[0].ID != two.ID {
		t.Fatalf("after remove: %+v", after)
	}

	// Cleanup orphan favorites via listing delete cascade is covered in
	// favorite package tests; here just delete the listings.
	_ = doJSON(t, mux, http.MethodDelete, "/listings/"+one.ID, "", owner)
	_ = doJSON(t, mux, http.MethodDelete, "/listings/"+two.ID, "", owner)

	// Silence unused import.
	var _ = sql.ErrNoRows
}
