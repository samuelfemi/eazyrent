package favorite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/femi/golang-easyrent/internal/auth"
	"github.com/femi/golang-easyrent/internal/listing"
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

func testUser(t *testing.T, db *sql.DB, tag string) auth.User {
	t.Helper()
	suffix := time.Now().UnixNano()
	u, err := auth.NewStore(db).CreateUser(
		context.Background(),
		fmt.Sprintf("fav-%s-%d@example.com", tag, suffix),
		fmt.Sprintf("+234%013d", suffix%10000000000000),
		"hash-placeholder",
		"Fav "+tag,
	)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, u.ID)
	})
	return u
}

func createListing(t *testing.T, db *sql.DB, landlordID uuid.UUID, title string) listing.Listing {
	t.Helper()
	l, err := listing.NewStore(db).Create(
		context.Background(), landlordID, title, "desc for fav test listing",
		"100000", 2, false, 6.5, 3.3, "Lagos",
	)
	if err != nil {
		t.Fatalf("Create listing: %v", err)
	}
	return l
}

func TestFavoriteAddRemoveList(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	user := testUser(t, db, "addr")
	landlord := testUser(t, db, "landlord")
	store := NewStore(db)
	svc := NewService(store)

	a := createListing(t, db, landlord.ID, fmt.Sprintf("FavA-%d", time.Now().UnixNano()))
	b := createListing(t, db, landlord.ID, fmt.Sprintf("FavB-%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM listings WHERE id = ANY($1::uuid[])`,
			[]string{a.ID.String(), b.ID.String()})
	})

	if err := svc.Add(ctx, user.ID, a.ID); err != nil {
		t.Fatalf("Add a: %v", err)
	}
	if ok, _ := svc.IsFavorited(ctx, user.ID, a.ID); !ok {
		t.Fatal("a should be favorited")
	}
	// Idempotent second add.
	if err := svc.Add(ctx, user.ID, a.ID); err != nil {
		t.Fatalf("Add a again: %v", err)
	}
	if err := svc.Add(ctx, user.ID, b.ID); err != nil {
		t.Fatalf("Add b: %v", err)
	}

	page, err := svc.List(ctx, user.ID, 1, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("want 2 favorites, got %+v", page)
	}
	// Newest favorite first: b then a.
	if page.Data[0].ID != b.ID {
		t.Fatalf("order: want %v first, got %v", b.ID, page.Data[0].ID)
	}
	if page.Data[0].FavoriteCount != 1 || page.Data[1].FavoriteCount != 1 {
		t.Fatalf("favorite counts: %+v", page.Data)
	}

	if err := svc.Remove(ctx, user.ID, a.ID); err != nil {
		t.Fatalf("Remove a: %v", err)
	}
	if ok, _ := svc.IsFavorited(ctx, user.ID, a.ID); ok {
		t.Fatal("a should not be favorited after remove")
	}
	// Idempotent second remove.
	if err := svc.Remove(ctx, user.ID, a.ID); err != nil {
		t.Fatalf("Remove a again: %v", err)
	}

	page, _ = svc.List(ctx, user.ID, 1, 10)
	if page.Total != 1 || page.Data[0].ID != b.ID {
		t.Fatalf("after remove: %+v", page)
	}

	// Unknown listing -> 404 via ErrListingNotFound.
	if err := svc.Add(ctx, user.ID, uuid.New()); err == nil {
		t.Fatal("Add unknown listing should fail")
	}
}

func TestFavoriteCascadeOnListingDelete(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	user := testUser(t, db, "cascade-user")
	landlord := testUser(t, db, "cascade-landlord")
	store := NewStore(db)
	svc := NewService(store)

	l := createListing(t, db, landlord.ID, fmt.Sprintf("FavCascade-%d", time.Now().UnixNano()))
	if err := svc.Add(ctx, user.ID, l.ID); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM listings WHERE id = $1`, l.ID); err != nil {
		t.Fatalf("delete listing: %v", err)
	}
	page, err := svc.List(ctx, user.ID, 1, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("favorites should cascade on listing delete, got %+v", page)
	}
}
