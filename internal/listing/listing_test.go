package listing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/femi/golang-easyrent/internal/auth"
	"github.com/google/uuid"

	_ "github.com/jackc/pgx/v5/stdlib"
)

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

func testLandlord(t *testing.T, db *sql.DB, tag string) auth.User {
	t.Helper()

	suffix := time.Now().UnixNano()
	users := auth.NewStore(db)
	u, err := users.CreateUser(
		context.Background(),
		fmt.Sprintf("landlord-%s-%d@example.com", tag, suffix),
		fmt.Sprintf("+234%013d", suffix%10000000000000),
		"hash-placeholder",
		"Landlord "+tag,
	)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, u.ID)
	})
	return u
}

func testService(db *sql.DB) Service {
	return NewService(NewStore(db))
}

func createListing(t *testing.T, svc Service, landlordID uuid.UUID, title string) Listing {
	t.Helper()

	l, err := svc.Create(context.Background(), CreateListingParams{
		LandlordID:  landlordID,
		Title:       title,
		Description: "Very big house with parking",
		Price:       "120000",
		Rooms:       3,
		Furnished:   false,
		Latitude:    6.5244,
		Longitude:   3.3792,
		Address:     "Lagos, Nigeria",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return l
}

func TestListingLifecycle(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	svc := testService(db)
	landlord := testLandlord(t, db, "lifecycle")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM listings WHERE landlord_id = $1`, landlord.ID)
	})

	created := createListing(t, svc, landlord.ID, "New Bedroom Flat")
	if created.Title != "New Bedroom Flat" {
		t.Fatalf("Title: got %q", created.Title)
	}
	if created.Address != "Lagos, Nigeria" {
		t.Fatalf("Address: got %q", created.Address)
	}
	if created.LandlordID != landlord.ID {
		t.Fatalf("LandlordID: got %v", created.LandlordID)
	}
	if !created.Rooms.Valid || created.Rooms.Int32 != 3 {
		t.Fatalf("Rooms: got %+v", created.Rooms)
	}
	if created.FavoriteCount != 0 {
		t.Fatalf("FavoriteCount: got %d", created.FavoriteCount)
	}
	if created.Status != "avaiable" {
		t.Fatalf("Status: got %q", created.Status)
	}
	if created.Latitude != 6.5244 || created.Longitude != 3.3792 {
		t.Fatalf("coordinates: got %v, %v", created.Latitude, created.Longitude)
	}

	got, err := svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Listing.ID != created.ID {
		t.Fatalf("GetByID wrong listing: %v", got.Listing.ID)
	}
	if len(got.Media) != 0 {
		t.Fatalf("new listing should have no media, got %d", len(got.Media))
	}
	if !got.LandlordPhone.Valid || !got.LandlordName.Valid {
		t.Fatalf("detail should carry landlord contact: %+v", got)
	}

	if _, err := svc.GetByID(ctx, uuid.New()); !errors.Is(err, ErrListingNotFound) {
		t.Fatalf("GetByID unknown: want ErrListingNotFound, got %v", err)
	}

	updated, err := svc.Update(ctx, created.ID, landlord.ID, UpdateParams{
		Title: strptr("New Title"),
		Price: strptr("200000"),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Title != "New Title" || updated.Price != "200000.00" {
		t.Fatalf("Update did not apply: %+v", updated)
	}

	other := testLandlord(t, db, "intruder")
	if _, err := svc.Update(ctx, created.ID, other.ID, UpdateParams{Title: strptr("Hacked")}); !errors.Is(err, ErrListingForbidden) {
		t.Fatalf("Update by non-owner: want ErrListingForbidden, got %v", err)
	}
	if err := svc.Delete(ctx, created.ID, other.ID); !errors.Is(err, ErrListingForbidden) {
		t.Fatalf("Delete by non-owner: want ErrListingForbidden, got %v", err)
	}

	if err := svc.Delete(ctx, created.ID, landlord.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.GetByID(ctx, created.ID); !errors.Is(err, ErrListingNotFound) {
		t.Fatalf("GetByID after delete: want ErrListingNotFound, got %v", err)
	}
}

func strptr(s string) *string { return &s }

func TestListingMedia(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	svc := testService(db)
	landlord := testLandlord(t, db, "media")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM listings WHERE landlord_id = $1`, landlord.ID)
	})

	l := createListing(t, svc, landlord.ID, "Media Flat")

	first, err := svc.AddMedia(ctx, l.ID, landlord.ID, "https://cdn.example.com/b.jpg", "image", 2)
	if err != nil {
		t.Fatalf("AddMedia: %v", err)
	}
	second, err := svc.AddMedia(ctx, l.ID, landlord.ID, "https://cdn.example.com/a.jpg", "image", 1)
	if err != nil {
		t.Fatalf("AddMedia: %v", err)
	}

	got, err := svc.GetByID(ctx, l.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if len(got.Media) != 2 || got.Media[0].ID != second.ID || got.Media[1].ID != first.ID {
		t.Fatalf("media should come back ordered, got %+v", got.Media)
	}

	page, err := svc.GetAll(ctx, 1, 10, Filters{})
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	found := false
	for _, item := range page.Data {
		if item.ID == l.ID {
			found = true
			if !item.CoverImage.Valid || item.CoverImage.String != second.URL {
				t.Fatalf("cover should be the lowest-order media: %+v", item.CoverImage)
			}
		}
	}
	if !found {
		t.Fatal("created listing missing from GetAll")
	}

	other := testLandlord(t, db, "media-intruder")
	if _, err := svc.AddMedia(ctx, l.ID, other.ID, "https://cdn.example.com/evil.jpg", "image", 3); !errors.Is(err, ErrListingForbidden) {
		t.Fatalf("AddMedia by non-owner: want ErrListingForbidden, got %v", err)
	}

	if err := svc.DeleteMedia(ctx, first.ID); err != nil {
		t.Fatalf("DeleteMedia: %v", err)
	}
	got, err = svc.GetByID(ctx, l.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if len(got.Media) != 1 || got.Media[0].ID != second.ID {
		t.Fatalf("media not deleted: %+v", got.Media)
	}
}

func TestListingFindAll(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	svc := testService(db)
	landlord := testLandlord(t, db, "search")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM listings WHERE landlord_id = $1`, landlord.ID)
	})
	prefix := fmt.Sprintf("FindAll%d", time.Now().UnixNano())

	furnished := true
	a := createListing(t, svc, landlord.ID, prefix+" Lekki Luxury Duplex")
	if _, err := svc.UpdateStatus(ctx, a.ID, landlord.ID, "rented"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	b, err := svc.Create(ctx, CreateListingParams{
		LandlordID: landlord.ID, Title: prefix + " Ikeja Mini Flat", Description: "Cozy place near the GRA",
		Price: "80000", Rooms: 1, Furnished: true, Latitude: 6.60, Longitude: 3.35, Address: "Ikeja GRA",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	c, err := svc.Create(ctx, CreateListingParams{
		LandlordID: landlord.ID, Title: prefix + " Surulere Room", Description: "Room with parking space",
		Price: "120000", Rooms: 3, Furnished: false, Latitude: 6.50, Longitude: 3.35, Address: "Surulere",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = c

	rented, err := svc.GetAll(ctx, 1, 10, Filters{Status: "rented", Search: prefix})
	if err != nil {
		t.Fatalf("GetAll status: %v", err)
	}
	if rented.Total != 1 || rented.Data[0].ID != a.ID {
		t.Fatalf("status filter: got %+v", rented)
	}

	furn, err := svc.GetAll(ctx, 1, 10, Filters{Furnished: &furnished, Search: prefix})
	if err != nil {
		t.Fatalf("GetAll furnished: %v", err)
	}
	if furn.Total != 1 || furn.Data[0].ID != b.ID {
		t.Fatalf("furnished filter: got %+v", furn)
	}

	search, err := svc.GetAll(ctx, 1, 10, Filters{Search: strings.ToLower(prefix) + " lekki"})
	if err != nil {
		t.Fatalf("GetAll search: %v", err)
	}
	if search.Total != 1 || search.Data[0].ID != a.ID {
		t.Fatalf("search should match title case-insensitively: %+v", search)
	}

	minPrice, maxPrice := 90000.0, 130000.0
	priced, err := svc.GetAll(ctx, 1, 10, Filters{MinPrice: &minPrice, MaxPrice: &maxPrice, Search: prefix})
	if err != nil {
		t.Fatalf("GetAll price: %v", err)
	}
	if priced.Total != 2 {
		t.Fatalf("price band 90k-130k should match 2, got %d", priced.Total)
	}

	rooms := 3
	roomed, err := svc.GetAll(ctx, 1, 10, Filters{Rooms: &rooms, Search: prefix})
	if err != nil {
		t.Fatalf("GetAll rooms: %v", err)
	}
	if roomed.Total != 2 {
		t.Fatalf("rooms=3 should match 2, got %d", roomed.Total)
	}

	p1, err := svc.GetAll(ctx, 1, 2, Filters{Search: prefix})
	if err != nil {
		t.Fatalf("GetAll page 1: %v", err)
	}
	p2, err := svc.GetAll(ctx, 2, 2, Filters{Search: prefix})
	if err != nil {
		t.Fatalf("GetAll page 2: %v", err)
	}
	if p1.Total != 3 || p1.TotalPages != 2 || len(p1.Data) != 2 || len(p2.Data) != 1 {
		t.Fatalf("pagination wrong: p1=%+v p2=%+v", p1, p2)
	}
	if p1.Data[0].ID == p2.Data[0].ID {
		t.Fatal("pages should not repeat rows")
	}
}

func TestListingFindByLandlord(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	svc := testService(db)
	mine := testLandlord(t, db, "mine")
	theirs := testLandlord(t, db, "theirs")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM listings WHERE landlord_id = ANY($1::uuid[])`,
			[]string{mine.ID.String(), theirs.ID.String()})
	})

	createListing(t, svc, mine.ID, "Mine One")
	createListing(t, svc, mine.ID, "Mine Two")
	createListing(t, svc, theirs.ID, "Theirs One")

	page, err := svc.GetMyListings(ctx, mine.ID, 1, 10)
	if err != nil {
		t.Fatalf("GetMyListings: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("want 2 of mine, got %+v", page)
	}
	for _, l := range page.Data {
		if l.LandlordID != mine.ID {
			t.Fatalf("leaked another landlord's listing: %+v", l)
		}
	}
}

func TestNormalizePagination(t *testing.T) {
	tests := []struct {
		name      string
		page      int
		limit     int
		wantPage  int
		wantLimit int
	}{
		{"ok", 2, 20, 2, 20},
		{"zero page", 0, 20, 1, 20},
		{"negative page", -3, 20, 1, 20},
		{"zero limit", 1, 0, 1, 1},
		{"over cap", 1, 500, 1, MaxPageSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, limit := NormalizePagination(tt.page, tt.limit)
			if page != tt.wantPage || limit != tt.wantLimit {
				t.Fatalf("got (%d, %d), want (%d, %d)", page, limit, tt.wantPage, tt.wantLimit)
			}
		})
	}
}
