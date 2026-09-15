package favorite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/femi/golang-easyrent/internal/listing"
	"github.com/google/uuid"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) Store {
	return Store{db: db}
}

const listingColumns = `l.id, l.landlord_id, l.title, l.description, l.price::text,
	l.rooms, l.furnished, l.status, l.address,
	ST_Y(l.location::geometry) AS latitude, ST_X(l.location::geometry) AS longitude,
	l.created_at, l.updated_at`

func scanListing(s scanner) (listing.Listing, error) {
	var l listing.Listing
	err := s.Scan(
		&l.ID,
		&l.LandlordID,
		&l.Title,
		&l.Description,
		&l.Price,
		&l.Rooms,
		&l.Furnished,
		&l.Status,
		&l.Address,
		&l.Latitude,
		&l.Longitude,
		&l.CreatedAt,
		&l.UpdatedAt,
	)
	return l, err
}

type scanner interface {
	Scan(dest ...any) error
}

func (s Store) Add(ctx context.Context, userID, listingID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO favorites (user_id, listing_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		userID, listingID)
	if err != nil {
		if strings.Contains(err.Error(), "violates foreign key") || strings.Contains(err.Error(), "23503") {
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
		SELECT `+listingColumns+`
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
		l, err := scanListing(rows)
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

	favCounts := map[uuid.UUID]int64{}
	covers := map[uuid.UUID]string{}
	if len(listings) > 0 {
		ids := make([]string, len(listings))
		for i, l := range listings {
			ids[i] = l.ID.String()
		}

		favRows, err := s.db.QueryContext(ctx, `
			SELECT listing_id, COUNT(*)
			FROM favorites
			WHERE listing_id = ANY($1::uuid[])
			GROUP BY listing_id`, ids)
		if err != nil {
			return listing.Page{}, err
		}
		for favRows.Next() {
			var id uuid.UUID
			var n int64
			if err := favRows.Scan(&id, &n); err != nil {
				favRows.Close()
				return listing.Page{}, err
			}
			favCounts[id] = n
		}
		favRows.Close()
		if err := favRows.Err(); err != nil {
			return listing.Page{}, err
		}

		coverRows, err := s.db.QueryContext(ctx, `
			SELECT DISTINCT ON (listing_id) listing_id, url
			FROM listing_media
			WHERE listing_id = ANY($1::uuid[])
			ORDER BY listing_id, "order"`, ids)
		if err != nil {
			return listing.Page{}, err
		}
		for coverRows.Next() {
			var id uuid.UUID
			var url string
			if err := coverRows.Scan(&id, &url); err != nil {
				coverRows.Close()
				return listing.Page{}, err
			}
			covers[id] = url
		}
		coverRows.Close()
		if err := coverRows.Err(); err != nil {
			return listing.Page{}, err
		}
	}

	for i, l := range listings {
		listings[i].FavoriteCount = favCounts[l.ID]
		if url, ok := covers[l.ID]; ok {
			listings[i].CoverImage = sql.NullString{String: url, Valid: true}
		}
	}

	return listing.Page{
		Data:       listings,
		Total:      total,
		Page:       page,
		Limit:      limit,
		TotalPages: (total + limit - 1) / limit,
	}, nil
}
