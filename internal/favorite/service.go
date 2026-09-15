package favorite

import (
	"context"

	"github.com/femi/golang-easyrent/internal/listing"
	"github.com/google/uuid"
)

type Service struct {
	store Store
}

func NewService(store Store) Service {
	return Service{store: store}
}

func (s Service) Add(ctx context.Context, userID, listingID uuid.UUID) error {
	return s.store.Add(ctx, userID, listingID)
}

func (s Service) Remove(ctx context.Context, userID, listingID uuid.UUID) error {
	return s.store.Remove(ctx, userID, listingID)
}

func (s Service) IsFavorited(ctx context.Context, userID, listingID uuid.UUID) (bool, error) {
	return s.store.IsFavorited(ctx, userID, listingID)
}

func (s Service) List(ctx context.Context, userID uuid.UUID, page, limit int) (listing.Page, error) {
	return s.store.List(ctx, userID, page, limit)
}

var _ = listing.ErrListingNotFound
