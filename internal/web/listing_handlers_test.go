package web

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/femi/golang-easyrent/internal/auth"
	"github.com/femi/golang-easyrent/internal/ratelimit"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// testLandlordWithToken creates a user and mints a bearer token for it,
// cleaning the user up (listings cascade) when the test ends.
func testLandlordWithToken(t *testing.T, db *sql.DB, h Handler, tag string) (string, string) {
	t.Helper()

	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("http-landlord-%s-%d@example.com", tag, suffix)
	phone := fmt.Sprintf("+234%013d", suffix%10000000000000)

	u, err := auth.NewStore(db).CreateUser(context.Background(), email, phone, "hash-placeholder", "Landlord "+tag)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, u.ID)
	})

	raw, err := h.AuthSvc.Tokens.SignAccessToken(u.ID, u.Email)
	if err != nil {
		t.Fatalf("SignAccessToken: %v", err)
	}
	return u.ID.String(), "Bearer " + raw
}

func createListingBody(title string) string {
	return fmt.Sprintf(`{"title":%q,"description":"Very big house with parking space","price":"150000","rooms":3,"furnished":false,"latitude":6.5244,"longitude":3.3792,"address":"Lagos, Nigeria"}`, title)
}

// TestCreateListingNumericPrice accepts a JSON number for price, the
// shape clients naturally send.
func TestCreateListingNumericPrice(t *testing.T) {
	h, db := testHandler(t)
	mux := h.Routes()
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM listings WHERE title = 'Numeric Price Flat'`)
	})

	_, owner := testLandlordWithToken(t, db, h, "numeric")

	body := `{"title":"Numeric Price Flat","description":"A spacious and well-maintained apartment in a serene neighborhood.","price":3500000,"rooms":3,"furnished":true,"latitude":6.4474,"longitude":3.4722,"address":"12 Admiralty Way, Lekki Phase 1, Lagos"}`
	rec := doJSON(t, mux, http.MethodPost, "/listings", body, owner)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with numeric price: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var created listingResponse
	decodeBody(t, rec, &created)
	if created.Price != "3500000.00" {
		t.Fatalf("price: got %q", created.Price)
	}

	rec = doJSON(t, mux, http.MethodPatch, "/listings/"+created.ID, `{"price":4000000}`, owner)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch with numeric price: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// TestListRateLimit proves the list endpoint trips to 429 with a
// Retry-After hint once its budget is spent.
func TestListRateLimit(t *testing.T) {
	h, _ := testHandler(t)
	h.Limits.List = ratelimit.NewLimiter(time.Hour, 1)
	mux := h.Routes()

	if rec := doJSON(t, mux, http.MethodGet, "/listings", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("first list: want 200, got %d", rec.Code)
	}
	rec := doJSON(t, mux, http.MethodGet, "/listings", "", "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second list: want 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 should carry a Retry-After header")
	}
}

// TestListingEndpoints runs the listing flow at HTTP level: auth gates,
// validation, CRUD, status, media and list filters through the mux.
func TestListingEndpoints(t *testing.T) {
	h, db := testHandler(t)
	mux := h.Routes()
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM listings WHERE title LIKE 'HTTP E2E%'`)
	})

	_, owner := testLandlordWithToken(t, db, h, "owner")
	_, intruder := testLandlordWithToken(t, db, h, "intruder")

	// Auth gate and validation.
	rec := doJSON(t, mux, http.MethodPost, "/listings", createListingBody("HTTP E2E Flat"), "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous create: want 401, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodPost, "/listings", `{"title":"x"}`, owner)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid create: want 400, got %d", rec.Code)
	}

	rec = doJSON(t, mux, http.MethodPost, "/listings", createListingBody("HTTP E2E Flat"), owner)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var created listingResponse
	decodeBody(t, rec, &created)
	if created.Title != "HTTP E2E Flat" || created.Price != "150000.00" {
		t.Fatalf("wrong create body: %+v", created)
	}
	if created.Rooms == nil || *created.Rooms != 3 {
		t.Fatalf("rooms missing: %+v", created)
	}
	listingID := created.ID

	// Detail: bad id, unknown id, ok.
	rec = doJSON(t, mux, http.MethodGet, "/listings/abc", "", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: want 400, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodGet, "/listings/00000000-0000-0000-0000-000000000000", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id: want 404, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodGet, "/listings/"+listingID, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var detail listingDetailResponse
	decodeBody(t, rec, &detail)
	if detail.Listing.ID != listingID || len(detail.Media) != 0 || detail.LandlordName == nil {
		t.Fatalf("wrong detail body: %+v", detail)
	}

	// List: public, filters, bad query.
	rec = doJSON(t, mux, http.MethodGet, "/listings?search=e2e", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var page listingPageResponse
	decodeBody(t, rec, &page)
	if page.Total < 1 {
		t.Fatalf("search should find the listing: %+v", page)
	}
	rec = doJSON(t, mux, http.MethodGet, "/listings?status=rented", "", "")
	decodeBody(t, rec, &page)
	if page.Total != 0 {
		t.Fatalf("rented filter should exclude it: %+v", page)
	}
	rec = doJSON(t, mux, http.MethodGet, "/listings?page=0", "", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad page: want 400, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodGet, "/listings/my", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous my: want 401, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodGet, "/listings/my", "", owner)
	decodeBody(t, rec, &page)
	if page.Total != 1 {
		t.Fatalf("my listings: want 1, got %+v", page)
	}
	rec = doJSON(t, mux, http.MethodGet, "/listings/my", "", intruder)
	decodeBody(t, rec, &page)
	if page.Total != 0 {
		t.Fatalf("intruder should see none: %+v", page)
	}

	// Update: forbidden, invalid, ok.
	rec = doJSON(t, mux, http.MethodPatch, "/listings/"+listingID, `{"title":"Hacked"}`, intruder)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("intruder update: want 403, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodPatch, "/listings/"+listingID, `{"price":"nope"}`, owner)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad price: want 400, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodPatch, "/listings/"+listingID, `{"title":"HTTP E2E Updated","price":"200000"}`, owner)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var updated listingResponse
	decodeBody(t, rec, &updated)
	if updated.Title != "HTTP E2E Updated" || updated.Price != "200000.00" {
		t.Fatalf("update not applied: %+v", updated)
	}

	// Status: invalid literal, forbidden, ok.
	rec = doJSON(t, mux, http.MethodPatch, "/listings/"+listingID+"/status", `{"status":"sold"}`, owner)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad status: want 400, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodPatch, "/listings/"+listingID+"/status", `{"status":"rented"}`, intruder)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("intruder status: want 403, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodPatch, "/listings/"+listingID+"/status", `{"status":"rented"}`, owner)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	decodeBody(t, rec, &updated)
	if updated.Status != "rented" {
		t.Fatalf("status not applied: %+v", updated)
	}

	// Media: forbidden, invalid, ok.
	rec = doJSON(t, mux, http.MethodPost, "/listings/"+listingID+"/media",
		`{"url":"https://cdn.example.com/x.jpg","type":"image","order":0}`, intruder)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("intruder media: want 403, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodPost, "/listings/"+listingID+"/media",
		`{"url":"x","type":"pdf","order":-1}`, owner)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad media: want 400, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodPost, "/listings/"+listingID+"/media",
		`{"url":"https://cdn.example.com/x.jpg","type":"image","order":0}`, owner)
	if rec.Code != http.StatusCreated {
		t.Fatalf("media: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Delete: forbidden, ok, then gone.
	rec = doJSON(t, mux, http.MethodDelete, "/listings/"+listingID, "", intruder)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("intruder delete: want 403, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodDelete, "/listings/"+listingID, "", owner)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: want 204, got %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodGet, "/listings/"+listingID, "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete: want 404, got %d", rec.Code)
	}
}
