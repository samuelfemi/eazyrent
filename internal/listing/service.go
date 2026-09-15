package listing

import (
	"context"

	"github.com/google/uuid"
)

type CreateListingParams struct {
	LandlordID  uuid.UUID
	Title       string
	Description string
	Price       string
	Rooms       int
	Furnished   bool
	Latitude    float64
	Longitude   float64
	Address     string
}

type Service struct {
	Listings Store
}

func NewService(store Store) Service {
	return Service{Listings: store}
}

func assertOwner(l Listing, landlordID uuid.UUID) error {
	if l.LandlordID != landlordID {
		return ErrListingForbidden
	}
	return nil
}

func (s Service) Create(ctx context.Context, p CreateListingParams) (Listing, error) {
	return s.Listings.Create(ctx, p.LandlordID, p.Title, p.Description, p.Price,
		p.Rooms, p.Furnished, p.Latitude, p.Longitude, p.Address)
}

func (s Service) GetByID(ctx context.Context, id uuid.UUID) (Detail, error) {
	return s.Listings.FindDetail(ctx, id)
}

func (s Service) GetAll(ctx context.Context, page, limit int, f Filters) (Page, error) {
	return s.Listings.FindAll(ctx, page, limit, f)
}

func (s Service) GetMyListings(ctx context.Context, landlordID uuid.UUID, page, limit int) (Page, error) {
	return s.Listings.FindByLandlord(ctx, landlordID, page, limit)
}

func (s Service) AddMedia(ctx context.Context, listingID, landlordID uuid.UUID, url, mediaType string, order int) (Media, error) {
	l, err := s.Listings.FindByID(ctx, listingID)
	if err != nil {
		return Media{}, err
	}
	if err := assertOwner(l, landlordID); err != nil {
		return Media{}, err
	}
	return s.Listings.AddMedia(ctx, listingID, url, mediaType, order)
}

func (s Service) DeleteMedia(ctx context.Context, mediaID uuid.UUID) error {
	return s.Listings.DeleteMedia(ctx, mediaID)
}

func (s Service) Update(ctx context.Context, id, landlordID uuid.UUID, p UpdateParams) (Listing, error) {
	l, err := s.Listings.FindByID(ctx, id)
	if err != nil {
		return Listing{}, err
	}
	if err := assertOwner(l, landlordID); err != nil {
		return Listing{}, err
	}
	return s.Listings.Update(ctx, id, p)
}

func (s Service) UpdateStatus(ctx context.Context, id, landlordID uuid.UUID, status string) (Listing, error) {
	return s.Update(ctx, id, landlordID, UpdateParams{Status: &status})
}

func (s Service) Delete(ctx context.Context, id, landlordID uuid.UUID) error {
	l, err := s.Listings.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := assertOwner(l, landlordID); err != nil {
		return err
	}
	return s.Listings.Delete(ctx, id)
}
