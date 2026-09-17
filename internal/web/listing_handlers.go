package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/femi/golang-easyrent/internal/listing"
	"github.com/google/uuid"
)

// validStatus reports whether s is one of the listing statuses. The
// spellings are verbatim from the original schema and migration.
func validStatus(s string) bool {
	return s == "avaiable" || s == "rented" || s == "inative"
}

// listingResponse is the public shape of a listing: nullable columns as
// JSON null, IDs as strings, timestamps as RFC3339.
type listingResponse struct {
	ID            string    `json:"id"`
	LandlordID    string    `json:"landlord_id"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	Price         string    `json:"price"`
	Rooms         *int32    `json:"rooms"`
	Furnished     bool      `json:"furnished"`
	Status        string    `json:"status"`
	Address       string    `json:"address"`
	Latitude      float64   `json:"latitude"`
	Longitude     float64   `json:"longitude"`
	CoverImage    *string   `json:"cover_image"`
	FavoriteCount int64     `json:"favorite_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func toListingResponse(l listing.Listing) listingResponse {
	var rooms *int32
	if l.Rooms.Valid {
		rooms = &l.Rooms.Int32
	}
	var cover *string
	if l.CoverImage.Valid {
		cover = &l.CoverImage.String
	}
	return listingResponse{
		ID:            l.ID.String(),
		LandlordID:    l.LandlordID.String(),
		Title:         l.Title,
		Description:   l.Description,
		Price:         l.Price,
		Rooms:         rooms,
		Furnished:     l.Furnished,
		Status:        l.Status,
		Address:       l.Address,
		Latitude:      l.Latitude,
		Longitude:     l.Longitude,
		CoverImage:    cover,
		FavoriteCount: l.FavoriteCount,
		CreatedAt:     l.CreatedAt,
		UpdatedAt:     l.UpdatedAt,
	}
}

type mediaResponse struct {
	ID        string    `json:"id"`
	ListingID string    `json:"listing_id"`
	URL       string    `json:"url"`
	Type      string    `json:"type"`
	Order     int       `json:"order"`
	CreatedAt time.Time `json:"created_at"`
}

func toMediaResponse(m listing.Media) mediaResponse {
	return mediaResponse{
		ID:        m.ID.String(),
		ListingID: m.ListingID.String(),
		URL:       m.URL,
		Type:      m.Type,
		Order:     m.Order,
		CreatedAt: m.CreatedAt,
	}
}

type listingDetailResponse struct {
	Listing       listingResponse `json:"listing"`
	Media         []mediaResponse `json:"media"`
	LandlordPhone *string         `json:"landlord_phone"`
	LandlordName  *string         `json:"landlord_name"`
}

func toDetailResponse(d listing.Detail) listingDetailResponse {
	media := make([]mediaResponse, len(d.Media))
	for i, m := range d.Media {
		media[i] = toMediaResponse(m)
	}
	var phone, name *string
	if d.LandlordPhone.Valid {
		phone = &d.LandlordPhone.String
	}
	if d.LandlordName.Valid {
		name = &d.LandlordName.String
	}
	return listingDetailResponse{
		Listing:       toListingResponse(d.Listing),
		Media:         media,
		LandlordPhone: phone,
		LandlordName:  name,
	}
}

type listingPageResponse struct {
	Data       []listingResponse `json:"data"`
	Total      int               `json:"total"`
	Page       int               `json:"page"`
	Limit      int               `json:"limit"`
	TotalPages int               `json:"total_pages"`
}

func toPageResponse(p listing.Page) listingPageResponse {
	data := make([]listingResponse, len(p.Data))
	for i, l := range p.Data {
		data[i] = toListingResponse(l)
	}
	return listingPageResponse{
		Data:       data,
		Total:      p.Total,
		Page:       p.Page,
		Limit:      p.Limit,
		TotalPages: p.TotalPages,
	}
}

// pathListingID parses the {id} path value. An unparseable id is a client
// error, never a 404.
func pathListingID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.UUID{}, errors.New("invalid listing id")
	}
	return id, nil
}

// maxPrice mirrors listings.price NUMERIC(14,2): anything larger is a
// typo, and rejecting it here turns a DB overflow 500 into a 400.
const maxPrice = 999999999999.99

func parsePrice(s string) error {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n < 0 {
		return errors.New("price must be a positive number")
	}
	if n > maxPrice {
		return errors.New("price is too large (max 999,999,999,999.99)")
	}
	return nil
}

// PriceString is a price that accepts a JSON string or number and keeps
// the text form, since clients naturally send 3500000 instead of "3500000".
type PriceString string

func (p *PriceString) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*p = PriceString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return errors.New("price must be a string or number")
	}
	*p = PriceString(n.String())
	return nil
}

type createListingRequest struct {
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Price       PriceString `json:"price"`
	Rooms       *int        `json:"rooms"`
	Furnished   *bool       `json:"furnished"`
	Latitude    *float64    `json:"latitude"`
	Longitude   *float64    `json:"longitude"`
	Address     string      `json:"address"`
}

func (r createListingRequest) validate() error {
	if len(r.Title) < 3 {
		return errors.New("title must be at least 3 characters")
	}
	if len(r.Description) < 10 {
		return errors.New("description must be at least 10 characters")
	}
	if err := parsePrice(string(r.Price)); err != nil {
		return err
	}
	if r.Rooms == nil || *r.Rooms < 0 {
		return errors.New("rooms is required and must be >= 0")
	}
	if r.Furnished == nil {
		return errors.New("furnished is required")
	}
	if r.Latitude == nil || *r.Latitude < -90 || *r.Latitude > 90 {
		return errors.New("latitude must be between -90 and 90")
	}
	if r.Longitude == nil || *r.Longitude < -180 || *r.Longitude > 180 {
		return errors.New("longitude must be between -180 and 180")
	}
	if strings.TrimSpace(r.Address) == "" {
		return errors.New("address is required")
	}
	return nil
}

// createListing godoc
//
//	@Summary		Create listing
//	@Description	Create a listing for the authenticated landlord.
//	@Tags			listings
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			body	body		createListingRequest	true	"Listing"
//	@Success		201		{object}	listingResponse
//	@Failure		400		{object}	errorResponse
//	@Failure		401		{object}	errorResponse
//	@Failure		429		{object}	errorResponse
//	@Router			/listings [post]
func (h Handler) createListing(w http.ResponseWriter, r *http.Request) {
	cu, ok := CurrentUserOf(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}

	var req createListingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Description = strings.TrimSpace(req.Description)
	req.Address = strings.TrimSpace(req.Address)
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	l, err := h.Listings.Create(r.Context(), listing.CreateListingParams{
		LandlordID:  cu.UserID,
		Title:       req.Title,
		Description: req.Description,
		Price:       string(req.Price),
		Rooms:       *req.Rooms,
		Furnished:   *req.Furnished,
		Latitude:    *req.Latitude,
		Longitude:   *req.Longitude,
		Address:     req.Address,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toListingResponse(l))
}

// getListing godoc
//
//	@Summary		Get listing
//	@Description	Get one listing with media and landlord contact.
//	@Tags			listings
//	@Produce		json
//	@Param			id	path		string	true	"Listing ID"
//	@Success		200	{object}	listingDetailResponse
//	@Failure		400	{object}	errorResponse
//	@Failure		404	{object}	errorResponse
//	@Failure		429	{object}	errorResponse
//	@Router			/listings/{id} [get]
func (h Handler) getListing(w http.ResponseWriter, r *http.Request) {
	id, err := pathListingID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	d, err := h.Listings.GetByID(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	detail := toDetailResponse(d)
	// Unverified / anonymous callers can browse listings but must not see
	// landlord contact — they have to verify email first.
	if cu, ok := CurrentUserOf(r); ok && cu.EmailVerified {
		// already verified via context (if wrapped) — keep contact
	} else if h.Auth.IsVerifiedRequest(r) {
		// verified via bearer token without RequireAuth wrapper
	} else {
		detail.LandlordPhone = nil
		detail.LandlordName = nil
	}
	writeJSON(w, http.StatusOK, detail)
}

// parsePageQuery reads page/limit like the TS NumberFromString schema:
// missing means defaults, malformed or out of range is a 400.
func parsePageQuery(r *http.Request) (int, int, error) {
	page, limit := 1, 20
	if raw := r.URL.Query().Get("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return 0, 0, errors.New("page must be >= 1")
		}
		page = n
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > listing.MaxPageSize {
			return 0, 0, errors.New("limit must be between 1 and 100")
		}
		limit = n
	}
	return page, limit, nil
}

// parseFilters reads the list filters. furnished follows the TS rule:
// anything but "true"/"false" means unset.
func parseFilters(r *http.Request) (listing.Filters, error) {
	q := r.URL.Query()
	var f listing.Filters

	if raw := q.Get("status"); raw != "" {
		if !validStatus(raw) {
			return f, errors.New("invalid status")
		}
		f.Status = raw
	}
	switch q.Get("furnished") {
	case "true":
		t := true
		f.Furnished = &t
	case "false":
		t := false
		f.Furnished = &t
	}
	if raw := q.Get("rooms"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return f, errors.New("rooms must be an integer")
		}
		f.Rooms = &n
	}
	if raw := q.Get("minRooms"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return f, errors.New("minRooms must be an integer")
		}
		f.MinRooms = &n
	}
	if raw := q.Get("minPrice"); raw != "" {
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil || n < 0 {
			return f, errors.New("minPrice must be >= 0")
		}
		f.MinPrice = &n
	}
	if raw := q.Get("maxPrice"); raw != "" {
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil || n < 0 {
			return f, errors.New("maxPrice must be >= 0")
		}
		f.MaxPrice = &n
	}
	f.Search = strings.TrimSpace(q.Get("search"))
	return f, nil
}

// listListings godoc
//
//	@Summary		List listings
//	@Description	Public paginated listing search with filters.
//	@Tags			listings
//	@Produce		json
//	@Param			page		query		int		false	"Page"
//	@Param			limit		query		int		false	"Page size (1-100)"
//	@Param			status		query		string	false	"avaiable, rented or inative"
//	@Param			furnished	query		string	false	"true or false"
//	@Param			rooms		query		int		false	"Exact rooms"
//	@Param			minRooms	query		int		false	"Minimum rooms"
//	@Param			minPrice	query		number	false	"Minimum price"
//	@Param			maxPrice	query		number	false	"Maximum price"
//	@Param			search		query		string	false	"Title, address and description search"
//	@Success		200			{object}	listingPageResponse
//	@Failure		400			{object}	errorResponse
//	@Failure		429			{object}	errorResponse
//	@Router			/listings [get]
func (h Handler) listListings(w http.ResponseWriter, r *http.Request) {
	page, limit, err := parsePageQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	filters, err := parseFilters(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	p, err := h.Listings.GetAll(r.Context(), page, limit, filters)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toPageResponse(p))
}

// myListings godoc
//
//	@Summary		My listings
//	@Description	Paginated listings of the authenticated landlord.
//	@Tags			listings
//	@Produce		json
//	@Security		Bearer
//	@Param			page	query		int	false	"Page"
//	@Param			limit	query		int	false	"Page size (1-100)"
//	@Success		200		{object}	listingPageResponse
//	@Failure		400		{object}	errorResponse
//	@Failure		401		{object}	errorResponse
//	@Router			/listings/my [get]
func (h Handler) myListings(w http.ResponseWriter, r *http.Request) {
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

	p, err := h.Listings.GetMyListings(r.Context(), cu.UserID, page, limit)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toPageResponse(p))
}

type updateListingRequest struct {
	Title       *string      `json:"title"`
	Description *string      `json:"description"`
	Price       *PriceString `json:"price"`
	Rooms       *int         `json:"rooms"`
	Furnished   *bool        `json:"furnished"`
	Latitude    *float64     `json:"latitude"`
	Longitude   *float64     `json:"longitude"`
	Address     *string      `json:"address"`
}

func (r updateListingRequest) validate() error {
	if r.Title != nil && len(strings.TrimSpace(*r.Title)) < 3 {
		return errors.New("title must be at least 3 characters")
	}
	if r.Description != nil && len(strings.TrimSpace(*r.Description)) < 10 {
		return errors.New("description must be at least 10 characters")
	}
	if r.Price != nil {
		if err := parsePrice(string(*r.Price)); err != nil {
			return err
		}
	}
	if r.Rooms != nil && *r.Rooms < 0 {
		return errors.New("rooms must be >= 0")
	}
	if (r.Latitude == nil) != (r.Longitude == nil) {
		return errors.New("latitude and longitude must be set together")
	}
	if r.Latitude != nil && (*r.Latitude < -90 || *r.Latitude > 90) {
		return errors.New("latitude must be between -90 and 90")
	}
	if r.Longitude != nil && (*r.Longitude < -180 || *r.Longitude > 180) {
		return errors.New("longitude must be between -180 and 180")
	}
	if r.Address != nil && strings.TrimSpace(*r.Address) == "" {
		return errors.New("address must not be empty")
	}
	return nil
}

func (r updateListingRequest) params() listing.UpdateParams {
	p := listing.UpdateParams{
		Title:       r.Title,
		Description: r.Description,
		Rooms:       r.Rooms,
		Furnished:   r.Furnished,
		Address:     r.Address,
		Latitude:    r.Latitude,
		Longitude:   r.Longitude,
	}
	if r.Price != nil {
		price := string(*r.Price)
		p.Price = &price
	}
	if p.Title != nil {
		trimmed := strings.TrimSpace(*p.Title)
		p.Title = &trimmed
	}
	if p.Description != nil {
		trimmed := strings.TrimSpace(*p.Description)
		p.Description = &trimmed
	}
	if p.Address != nil {
		trimmed := strings.TrimSpace(*p.Address)
		p.Address = &trimmed
	}
	return p
}

// updateListing godoc
//
//	@Summary		Update listing
//	@Description	Partial update of the caller's listing.
//	@Tags			listings
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			id		path		string					true	"Listing ID"
//	@Param			body	body		updateListingRequest	true	"Fields to change"
//	@Success		200		{object}	listingResponse
//	@Failure		400		{object}	errorResponse
//	@Failure		401		{object}	errorResponse
//	@Failure		403		{object}	errorResponse
//	@Failure		404		{object}	errorResponse
//	@Router			/listings/{id} [patch]
func (h Handler) updateListing(w http.ResponseWriter, r *http.Request) {
	cu, ok := CurrentUserOf(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	id, err := pathListingID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req updateListingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	l, err := h.Listings.Update(r.Context(), id, cu.UserID, req.params())
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toListingResponse(l))
}

// deleteListing godoc
//
//	@Summary		Delete listing
//	@Description	Delete the caller's listing; media and favorites cascade.
//	@Tags			listings
//	@Security		Bearer
//	@Param			id	path	string	true	"Listing ID"
//	@Success		204
//	@Failure		400	{object}	errorResponse
//	@Failure		401	{object}	errorResponse
//	@Failure		403	{object}	errorResponse
//	@Failure		404	{object}	errorResponse
//	@Router			/listings/{id} [delete]
func (h Handler) deleteListing(w http.ResponseWriter, r *http.Request) {
	cu, ok := CurrentUserOf(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	id, err := pathListingID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.Listings.Delete(r.Context(), id, cu.UserID); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type updateStatusRequest struct {
	Status string `json:"status"`
}

// updateListingStatus godoc
//
//	@Summary		Update listing status
//	@Description	Set avaiable, rented or inative on the caller's listing.
//	@Tags			listings
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			id		path		string					true	"Listing ID"
//	@Param			body	body		updateStatusRequest	true	"Status"
//	@Success		200		{object}	listingResponse
//	@Failure		400		{object}	errorResponse
//	@Failure		401		{object}	errorResponse
//	@Failure		403		{object}	errorResponse
//	@Failure		404		{object}	errorResponse
//	@Router			/listings/{id}/status [patch]
func (h Handler) updateListingStatus(w http.ResponseWriter, r *http.Request) {
	cu, ok := CurrentUserOf(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	id, err := pathListingID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req updateStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !validStatus(req.Status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}

	l, err := h.Listings.UpdateStatus(r.Context(), id, cu.UserID, req.Status)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toListingResponse(l))
}

type addMediaRequest struct {
	URL   string `json:"url"`
	Type  string `json:"type"`
	Order int    `json:"order"`
}

// addMedia godoc
//
//	@Summary		Add listing media
//	@Description	Attach an already-uploaded file URL (uploads go straight to the media provider).
//	@Tags			listings
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			id		path		string			true	"Listing ID"
//	@Param			body	body		addMediaRequest	true	"Media"
//	@Success		201		{object}	mediaResponse
//	@Failure		400		{object}	errorResponse
//	@Failure		401		{object}	errorResponse
//	@Failure		403		{object}	errorResponse
//	@Failure		404		{object}	errorResponse
//	@Router			/listings/{id}/media [post]
func (h Handler) addMedia(w http.ResponseWriter, r *http.Request) {
	cu, ok := CurrentUserOf(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	id, err := pathListingID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req addMediaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if u, err := url.ParseRequestURI(req.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeError(w, http.StatusBadRequest, "url must be a valid http(s) URL")
		return
	}
	if req.Type != "image" && req.Type != "video" {
		writeError(w, http.StatusBadRequest, "type must be image or video")
		return
	}
	if req.Order < 0 {
		writeError(w, http.StatusBadRequest, "order must be >= 0")
		return
	}

	m, err := h.Listings.AddMedia(r.Context(), id, cu.UserID, req.URL, req.Type, req.Order)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toMediaResponse(m))
}
