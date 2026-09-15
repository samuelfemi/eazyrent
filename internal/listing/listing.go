package listing

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const MaxPageSize = 100

type Listing struct {
	ID            uuid.UUID      `json:"id"`
	LandlordID    uuid.UUID      `json:"landlord_id"`
	Title         string         `json:"title"`
	Description   string         `json:"description"`
	Price         string         `json:"price"`
	Rooms         sql.NullInt32  `json:"rooms"`
	Furnished     bool           `json:"furnished"`
	Status        string         `json:"status"`
	Address       string         `json:"address"`
	Latitude      float64        `json:"latitude"`
	Longitude     float64        `json:"longitude"`
	CoverImage    sql.NullString `json:"cover_image"`
	FavoriteCount int64          `json:"favorite_count"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type Media struct {
	ID        uuid.UUID `json:"id"`
	ListingID uuid.UUID `json:"listing_id"`
	URL       string    `json:"url"`
	Type      string    `json:"type"`
	Order     int       `json:"order"`
	CreatedAt time.Time `json:"created_at"`
}

type Detail struct {
	Listing       Listing
	Media         []Media
	LandlordPhone sql.NullString
	LandlordName  sql.NullString
}

type Filters struct {
	Status    string
	Furnished *bool
	Rooms     *int
	MinRooms  *int
	MinPrice  *float64
	MaxPrice  *float64
	Search    string
}

type Page struct {
	Data       []Listing `json:"data"`
	Total      int       `json:"total"`
	Page       int       `json:"page"`
	Limit      int       `json:"limit"`
	TotalPages int       `json:"total_pages"`
}

type UpdateParams struct {
	Title       *string
	Description *string
	Price       *string
	Rooms       *int
	Furnished   *bool
	Status      *string
	Address     *string
	Latitude    *float64
	Longitude   *float64
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) Store {
	return Store{db: db}
}

func NormalizePagination(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 1
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	return page, limit
}

const listingColumns = `id, landlord_id, title, description, price::text,
	rooms, furnished, status, address,
	ST_Y(location::geometry) AS latitude, ST_X(location::geometry) AS longitude,
	created_at, updated_at`

type scanner interface {
	Scan(dest ...any) error
}

func scanListing(s scanner) (Listing, error) {
	var l Listing
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

const createListingQuery = `
	INSERT INTO listings (landlord_id, title, description, price, rooms, furnished, address, location)
	VALUES ($1, $2, $3, $4::numeric, $5, $6, $7, ST_SetSRID(ST_MakePoint($8, $9), 4326))
	RETURNING ` + listingColumns + `
`

func (s Store) Create(ctx context.Context, landlordID uuid.UUID, title, description, price string, rooms int, furnished bool, latitude, longitude float64, address string) (Listing, error) {
	l, err := scanListing(s.db.QueryRowContext(ctx, createListingQuery,
		landlordID, title, description, price, rooms, furnished, address, longitude, latitude))
	if err != nil {
		return Listing{}, err
	}
	return l, nil
}

const findListingByIDQuery = `
	SELECT ` + listingColumns + `
	FROM listings l
	WHERE l.id = $1
	LIMIT 1
`

const countFavoritesQuery = `
	SELECT COUNT(*)
	FROM favorites
	WHERE listing_id = $1
`

func (s Store) FindByID(ctx context.Context, id uuid.UUID) (Listing, error) {
	l, err := scanListing(s.db.QueryRowContext(ctx, findListingByIDQuery, id))
	if err == sql.ErrNoRows {
		return Listing{}, ErrListingNotFound
	}
	if err != nil {
		return Listing{}, err
	}

	if err := s.db.QueryRowContext(ctx, countFavoritesQuery, id).Scan(&l.FavoriteCount); err != nil {
		return Listing{}, err
	}
	return l, nil
}

const findDetailQuery = `
	SELECT l.id, l.landlord_id, l.title, l.description, l.price::text,
		l.rooms, l.furnished, l.status, l.address,
		ST_Y(l.location::geometry) AS latitude, ST_X(l.location::geometry) AS longitude,
		l.created_at, l.updated_at, u.phone, u.full_name
	FROM listings l
	LEFT JOIN users u ON u.id = l.landlord_id
	WHERE l.id = $1
	LIMIT 1
`

const findMediaQuery = `
	SELECT id, listing_id, url, type, "order", created_at
	FROM listing_media
	WHERE listing_id = $1
	ORDER BY "order"
`

func scanMedia(s scanner) (Media, error) {
	var m Media
	err := s.Scan(&m.ID, &m.ListingID, &m.URL, &m.Type, &m.Order, &m.CreatedAt)
	return m, err
}

func (s Store) FindDetail(ctx context.Context, id uuid.UUID) (Detail, error) {
	var d Detail
	err := s.db.QueryRowContext(ctx, findDetailQuery, id).Scan(
		&d.Listing.ID,
		&d.Listing.LandlordID,
		&d.Listing.Title,
		&d.Listing.Description,
		&d.Listing.Price,
		&d.Listing.Rooms,
		&d.Listing.Furnished,
		&d.Listing.Status,
		&d.Listing.Address,
		&d.Listing.Latitude,
		&d.Listing.Longitude,
		&d.Listing.CreatedAt,
		&d.Listing.UpdatedAt,
		&d.LandlordPhone,
		&d.LandlordName,
	)
	if err == sql.ErrNoRows {
		return Detail{}, ErrListingNotFound
	}
	if err != nil {
		return Detail{}, err
	}

	rows, err := s.db.QueryContext(ctx, findMediaQuery, id)
	if err != nil {
		return Detail{}, err
	}
	defer rows.Close()

	d.Media = []Media{}
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return Detail{}, err
		}
		d.Media = append(d.Media, m)
	}
	if err := rows.Err(); err != nil {
		return Detail{}, err
	}

	if err := s.db.QueryRowContext(ctx, countFavoritesQuery, id).Scan(&d.Listing.FavoriteCount); err != nil {
		return Detail{}, err
	}
	return d, nil
}

func (s Store) list(ctx context.Context, conds []string, args []any, page, limit int) (Page, error) {
	page, limit = NormalizePagination(page, limit)
	offset := (page - 1) * limit

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	selectQuery := `SELECT ` + listingColumns + `
		FROM listings l ` + where + `
		ORDER BY l.created_at DESC` + fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	rows, err := s.db.QueryContext(ctx, selectQuery, append(args, limit, offset)...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()

	listings := []Listing{}
	for rows.Next() {
		l, err := scanListing(rows)
		if err != nil {
			return Page{}, err
		}
		listings = append(listings, l)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}

	var total int64
	countQuery := `SELECT COUNT(*) FROM listings l ` + where
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return Page{}, err
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
			return Page{}, err
		}
		for favRows.Next() {
			var id uuid.UUID
			var n int64
			if err := favRows.Scan(&id, &n); err != nil {
				favRows.Close()
				return Page{}, err
			}
			favCounts[id] = n
		}
		favRows.Close()
		if err := favRows.Err(); err != nil {
			return Page{}, err
		}

		coverRows, err := s.db.QueryContext(ctx, `
			SELECT DISTINCT ON (listing_id) listing_id, url
			FROM listing_media
			WHERE listing_id = ANY($1::uuid[])
			ORDER BY listing_id, "order"`, ids)
		if err != nil {
			return Page{}, err
		}
		for coverRows.Next() {
			var id uuid.UUID
			var url string
			if err := coverRows.Scan(&id, &url); err != nil {
				coverRows.Close()
				return Page{}, err
			}
			covers[id] = url
		}
		coverRows.Close()
		if err := coverRows.Err(); err != nil {
			return Page{}, err
		}
	}

	for i, l := range listings {
		listings[i].FavoriteCount = favCounts[l.ID]
		if url, ok := covers[l.ID]; ok {
			listings[i].CoverImage = sql.NullString{String: url, Valid: true}
		}
	}

	return Page{
		Data:       listings,
		Total:      int(total),
		Page:       page,
		Limit:      limit,
		TotalPages: (int(total) + limit - 1) / limit,
	}, nil
}

func (s Store) FindAll(ctx context.Context, page, limit int, f Filters) (Page, error) {
	var conds []string
	var args []any

	next := func() int { return len(args) + 1 }

	if f.Status != "" {
		conds = append(conds, fmt.Sprintf("l.status = $%d::status", next()))
		args = append(args, f.Status)
	}
	if f.Furnished != nil {
		conds = append(conds, fmt.Sprintf("l.furnished = $%d", next()))
		args = append(args, *f.Furnished)
	}
	if f.Rooms != nil {
		conds = append(conds, fmt.Sprintf("l.rooms = $%d", next()))
		args = append(args, *f.Rooms)
	}
	if f.MinRooms != nil {
		conds = append(conds, fmt.Sprintf("l.rooms >= $%d", next()))
		args = append(args, *f.MinRooms)
	}
	if f.MinPrice != nil {
		conds = append(conds, fmt.Sprintf("l.price >= $%d::numeric", next()))
		args = append(args, *f.MinPrice)
	}
	if f.MaxPrice != nil {
		conds = append(conds, fmt.Sprintf("l.price <= $%d::numeric", next()))
		args = append(args, *f.MaxPrice)
	}
	if f.Search != "" {
		conds = append(conds, fmt.Sprintf("(l.title ILIKE $%d OR l.address ILIKE $%d OR l.description ILIKE $%d)", next(), next()+1, next()+2))
		term := "%" + f.Search + "%"
		args = append(args, term, term, term)
	}

	return s.list(ctx, conds, args, page, limit)
}

func (s Store) FindByLandlord(ctx context.Context, landlordID uuid.UUID, page, limit int) (Page, error) {
	return s.list(ctx, []string{"l.landlord_id = $1"}, []any{landlordID}, page, limit)
}

const addMediaQuery = `
	INSERT INTO listing_media (listing_id, url, type, "order")
	VALUES ($1, $2, $3::media_type, $4)
	RETURNING id, listing_id, url, type, "order", created_at
`

func (s Store) AddMedia(ctx context.Context, listingID uuid.UUID, url, mediaType string, order int) (Media, error) {
	m, err := scanMedia(s.db.QueryRowContext(ctx, addMediaQuery, listingID, url, mediaType, order))
	if err != nil {
		return Media{}, err
	}
	return m, nil
}

const deleteMediaQuery = `
	DELETE FROM listing_media
	WHERE id = $1
`

func (s Store) DeleteMedia(ctx context.Context, mediaID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, deleteMediaQuery, mediaID)
	return err
}

func (s Store) Update(ctx context.Context, id uuid.UUID, p UpdateParams) (Listing, error) {
	sets := []string{"updated_at = now()"}
	var args []any

	set := func(column string, value any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", column, len(args)+1))
		args = append(args, value)
	}

	if p.Title != nil {
		set("title", *p.Title)
	}
	if p.Description != nil {
		set("description", *p.Description)
	}
	if p.Price != nil {
		sets = append(sets, fmt.Sprintf("price = $%d::numeric", len(args)+1))
		args = append(args, *p.Price)
	}
	if p.Rooms != nil {
		set("rooms", *p.Rooms)
	}
	if p.Furnished != nil {
		set("furnished", *p.Furnished)
	}
	if p.Status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d::status", len(args)+1))
		args = append(args, *p.Status)
	}
	if p.Address != nil {
		set("address", *p.Address)
	}
	if p.Latitude != nil && p.Longitude != nil {
		sets = append(sets, fmt.Sprintf("location = ST_SetSRID(ST_MakePoint($%d, $%d), 4326)", len(args)+1, len(args)+2))
		args = append(args, *p.Longitude, *p.Latitude)
	}

	args = append(args, id)
	query := `UPDATE listings AS l SET ` + strings.Join(sets, ", ") +
		fmt.Sprintf(` WHERE l.id = $%d RETURNING `, len(args)) + listingColumns

	l, err := scanListing(s.db.QueryRowContext(ctx, query, args...))
	if err == sql.ErrNoRows {
		return Listing{}, ErrListingNotFound
	}
	if err != nil {
		return Listing{}, err
	}

	if err := s.db.QueryRowContext(ctx, countFavoritesQuery, id).Scan(&l.FavoriteCount); err != nil {
		return Listing{}, err
	}
	return l, nil
}

const deleteListingQuery = `
	DELETE FROM listings
	WHERE id = $1
`

func (s Store) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, deleteListingQuery, id)
	return err
}
