package app

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrContactReadUnavailable = errors.New("catalog: contact read repository and context are required")

type ContactInterest struct {
	NSID           string
	Name           string
	Username       string
	AvatarURL      *string
	Depth          int
	Starred        bool
	Status         string
	PhotosCrawled  int64
	PhotosTotal    int64
	FavoritesCount int64
	SizesPercent   int
	SizesConfirmed int64
	LastCheckAt    *time.Time
	UpdatedAt      *time.Time
	RecentPhotos   []ContactRecentPhoto
}
type ContactRecentPhoto struct {
	ID           string
	Alt          string
	ThumbnailURL *string
}
type (
	ContactInterestCounts struct{ All, Starred, Crawling, NeedsAttention int64 }
	ContactInterestPage   struct {
		Items      []ContactInterest
		Counts     ContactInterestCounts
		NextCursor *string
		UpdatedAt  *time.Time
	}
)

type ContactFacet struct {
	Name      string
	Completed int64
	Total     *int64
	Status    string
	UpdatedAt *time.Time
}
type (
	ContactFoundVia struct{ NSID, Label string }
	ContactDetail   struct {
		NSID, Username, RealName           string
		AvatarURL, CoverPhotoURL, Location *string
		Depth                              int
		FoundVia                           []ContactFoundVia
		Starred, Paused                    bool
		CrawlStatus                        string
		PhotoRecordsCrawled                int64
		PhotoRecordsTotal                  *int64
		SizesCompleted                     int
		Facets                             []ContactFacet
		Counts                             ContactCounts
		UpdatedAt                          *time.Time
	}
)

type (
	ContactCounts struct{ Photos, Favorites, Albums, Galleries, Groups int64 }
	ContactPhoto  struct {
		ID, Title      string
		ThumbnailURL   *string
		Width, Height  *int
		TakenAt        *time.Time
		SizesAvailable bool
	}
)

type ContactFavorite struct {
	ContactPhoto
	OwnerNSID                 string
	OwnerName, OwnerAvatarURL *string
}
type ContactPhotoPage struct {
	Items      []ContactPhoto
	Total      int64
	NextCursor *string
	UpdatedAt  *time.Time
}
type ContactFavoritePage struct {
	Items      []ContactFavorite
	Total      int64
	NextCursor *string
	UpdatedAt  *time.Time
}
type ContactAlbum struct {
	ID, Title, OwnerName    string
	StoredCount, TotalCount int64
	CoverPhotoURLs          []string
}
type ContactAlbumPage struct {
	Items     []ContactAlbum
	Total     int64
	UpdatedAt *time.Time
}
type ContactGallery struct {
	ID, Title  string
	PhotoCount int64
}
type ContactGalleryPage struct {
	Items     []ContactGallery
	Total     int64
	UpdatedAt *time.Time
}
type ContactGroup struct {
	NSID, Name       string
	MembersCount     int64
	PoolCrawlEnabled bool
}
type ContactGroupPage struct {
	Items     []ContactGroup
	Total     int64
	UpdatedAt *time.Time
}
type ContactAbout struct {
	LastChangeCheckAt             *time.Time
	Facets                        []ContactFacet
	TombstoneCount                int64
	TombstoneUpdatedAt, UpdatedAt *time.Time
}
type ContactPoolCrawl struct {
	Enabled   bool
	ChangedAt time.Time
}

type ContactReadRepository interface {
	ListContactInterests(context.Context, int64, string, string, string, string, int) (ContactInterestPage, error)
	GetContact(context.Context, int64, string) (ContactDetail, bool, error)
	ListContactPhotos(context.Context, int64, string, string, int) (ContactPhotoPage, error)
	ListContactFavorites(context.Context, int64, string, string, int) (ContactFavoritePage, error)
	ListContactAlbums(context.Context, int64, string) (ContactAlbumPage, error)
	ListContactGalleries(context.Context, int64, string) (ContactGalleryPage, error)
	ListContactGroups(context.Context, int64, string) (ContactGroupPage, error)
	GetContactAbout(context.Context, int64, string) (ContactAbout, bool, error)
	SetContactGroupPoolCrawl(context.Context, int64, string, string, bool) (ContactPoolCrawl, bool, error)
	PrioritiseContactSizes(context.Context, int64, string) (int64, error)
}

type ContactReads struct{ Repository ContactReadRepository }

func (s ContactReads) ListInterests(ctx context.Context, userID int64, filter, query, sort, cursor string, limit int) (ContactInterestPage, error) {
	if err := s.ready(ctx, userID); err != nil {
		return ContactInterestPage{}, err
	}
	if filter == "" {
		filter = "all"
	}
	if sort == "" {
		sort = "updated"
	}
	if filter != "all" && filter != "starred" && filter != "crawling" && filter != "needs_attention" {
		return ContactInterestPage{}, ErrInvalidPage
	}
	if sort != "updated" && sort != "name" && sort != "photos" {
		return ContactInterestPage{}, ErrInvalidPage
	}
	if len(query) > 100 {
		return ContactInterestPage{}, ErrInvalidPage
	}
	limit, err := normalizeLimit(limit)
	if err != nil {
		return ContactInterestPage{}, err
	}
	return s.Repository.ListContactInterests(ctx, userID, filter, strings.TrimSpace(query), sort, cursor, limit)
}

func (s ContactReads) GetContact(ctx context.Context, userID int64, nsid string) (ContactDetail, error) {
	if err := s.readyContact(ctx, userID, nsid); err != nil {
		return ContactDetail{}, err
	}
	value, ok, err := s.Repository.GetContact(ctx, userID, nsid)
	if err != nil {
		return ContactDetail{}, err
	}
	if !ok {
		return ContactDetail{}, ErrNotFound
	}
	return value, nil
}

func (s ContactReads) ListContactPhotos(ctx context.Context, userID int64, nsid, cursor string, limit int) (ContactPhotoPage, error) {
	if err := s.readyContact(ctx, userID, nsid); err != nil {
		return ContactPhotoPage{}, err
	}
	limit, err := normalizeLimit(limit)
	if err != nil {
		return ContactPhotoPage{}, err
	}
	return s.Repository.ListContactPhotos(ctx, userID, nsid, cursor, limit)
}

func (s ContactReads) ListContactFavorites(ctx context.Context, userID int64, nsid, cursor string, limit int) (ContactFavoritePage, error) {
	if err := s.readyContact(ctx, userID, nsid); err != nil {
		return ContactFavoritePage{}, err
	}
	limit, err := normalizeLimit(limit)
	if err != nil {
		return ContactFavoritePage{}, err
	}
	return s.Repository.ListContactFavorites(ctx, userID, nsid, cursor, limit)
}

func (s ContactReads) ListContactAlbums(ctx context.Context, userID int64, nsid string) (ContactAlbumPage, error) {
	if err := s.readyContact(ctx, userID, nsid); err != nil {
		return ContactAlbumPage{}, err
	}
	return s.Repository.ListContactAlbums(ctx, userID, nsid)
}

func (s ContactReads) ListContactGalleries(ctx context.Context, userID int64, nsid string) (ContactGalleryPage, error) {
	if err := s.readyContact(ctx, userID, nsid); err != nil {
		return ContactGalleryPage{}, err
	}
	return s.Repository.ListContactGalleries(ctx, userID, nsid)
}

func (s ContactReads) ListContactGroups(ctx context.Context, userID int64, nsid string) (ContactGroupPage, error) {
	if err := s.readyContact(ctx, userID, nsid); err != nil {
		return ContactGroupPage{}, err
	}
	return s.Repository.ListContactGroups(ctx, userID, nsid)
}

func (s ContactReads) GetContactAbout(ctx context.Context, userID int64, nsid string) (ContactAbout, error) {
	if err := s.readyContact(ctx, userID, nsid); err != nil {
		return ContactAbout{}, err
	}
	value, ok, err := s.Repository.GetContactAbout(ctx, userID, nsid)
	if err != nil {
		return ContactAbout{}, err
	}
	if !ok {
		return ContactAbout{}, ErrNotFound
	}
	return value, nil
}

func (s ContactReads) SetContactGroupPoolCrawl(ctx context.Context, userID int64, nsid, group string, enabled bool) (ContactPoolCrawl, error) {
	if err := s.readyContact(ctx, userID, nsid); err != nil {
		return ContactPoolCrawl{}, err
	}
	if strings.TrimSpace(group) == "" {
		return ContactPoolCrawl{}, ErrNotFound
	}
	value, ok, err := s.Repository.SetContactGroupPoolCrawl(ctx, userID, nsid, group, enabled)
	if err != nil {
		return ContactPoolCrawl{}, err
	}
	if !ok {
		return ContactPoolCrawl{}, ErrNotFound
	}
	return value, nil
}

func (s ContactReads) PrioritiseContactSizes(ctx context.Context, userID int64, nsid string) (int64, error) {
	if err := s.readyContact(ctx, userID, nsid); err != nil {
		return 0, err
	}
	return s.Repository.PrioritiseContactSizes(ctx, userID, nsid)
}

func (s ContactReads) ready(ctx context.Context, userID int64) error {
	if ctx == nil || s.Repository == nil || userID <= 0 {
		return ErrContactReadUnavailable
	}
	return nil
}

func (s ContactReads) readyContact(ctx context.Context, userID int64, nsid string) error {
	if err := s.ready(ctx, userID); err != nil {
		return err
	}
	if strings.TrimSpace(nsid) == "" || len(nsid) > 255 {
		return ErrNotFound
	}
	return nil
}
