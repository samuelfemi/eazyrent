package favorite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/femi/golang-easyrent/internal/listing"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// pgForeignKeyViolation is the Postgres error code for a foreign-key
// violation, used to turn a favorite of an unknown listing into 404.
const pgForeignKeyViolation = "23503"

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) Store {
	return Store{db: db}
}

func (s Store) Add(ctx context.Context, userID, listingID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO favorites (user_id, listing_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		userID, listingID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolation {
			return listing.ErrListingNotFound
		}
		return err
	}
	return nil
}

func (s Store) Remove(ctx context.Context, userID, listingID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM favorites WHERE user_id = $1 AND listing_id = $2`,
		userID, listingID)
	return err
}

func (s Store) IsFavorited(ctx context.Context, userID, listingID uuid.UUID) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM favorites WHERE user_id = $1 AND listing_id = $2)`,
		userID, listingID).Scan(&exists)
	return exists, err
}

func (s Store) List(ctx context.Context, userID uuid.UUID, page, limit int) (listing.Page, error) {
	page, limit = listing.NormalizePagination(page, limit)
	offset := (page - 1) * limit

	rows, err := s.db.QueryContext(ctx, `
		SELECT `+listing.Columns+`
		FROM listings l
		JOIN favorites f ON f.listing_id = l.id
		WHERE f.user_id = $1
		ORDER BY f.created_at DESC
		LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return listing.Page{}, err
	}
	defer rows.Close()

	listings := []listing.Listing{}
	for rows.Next() {
		l, err := listing.ScanListing(rows)
		if err != nil {
			return listing.Page{}, err
		}
		listings = append(listings, l)
	}
	if err := rows.Err(); err != nil {
		return listing.Page{}, err
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM favorites WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return listing.Page{}, err
	}

	if err := listing.AttachListMeta(ctx, s.db, listings); err != nil {
		return listing.Page{}, err
	}

	return listing.Page{
		Data:       listings,
		Total:      total,
		Page:       page,
		Limit:      limit,
		TotalPages: (total + limit - 1) / limit,
	}, nil
}
