package postgres

import (
	"context"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
)

// These adapters keep crawl's catalog values on the existing idempotent upsert paths.
func (r *Repository) UpsertFavorites(ctx context.Context, items []catalog.Favorite) error {
	values := make([]Favorite, 0, len(items))
	for _, item := range items {
		values = append(values, Favorite{PersonNSID: item.PersonNSID, PhotoID: item.PhotoID, FaveDate: item.FaveDate})
	}
	return r.UpsertFavoritesBatch(ctx, values)
}

func (r *Repository) UpsertAlbums(ctx context.Context, items []catalog.Album) error {
	values := make([]Album, 0, len(items))
	for _, item := range items {
		values = append(values, Album{ID: item.ID, OwnerNSID: item.OwnerNSID, Title: item.Title, Description: item.Description, PrimaryPhotoID: item.PrimaryPhotoID, PhotosCount: item.PhotosCount, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt})
	}
	return r.UpsertAlbumsBatch(ctx, values)
}

func (r *Repository) UpsertGalleries(ctx context.Context, items []catalog.Gallery) error {
	values := make([]Gallery, 0, len(items))
	for _, item := range items {
		values = append(values, Gallery{ID: item.ID, OwnerNSID: item.OwnerNSID, Title: item.Title, Description: item.Description, PrimaryPhotoID: item.PrimaryPhotoID, PhotosCount: item.PhotosCount, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt})
	}
	return r.UpsertGalleriesBatch(ctx, values)
}

func (r *Repository) UpsertGroups(ctx context.Context, items []catalog.Group) error {
	values := make([]Group, 0, len(items))
	for _, item := range items {
		values = append(values, Group{NSID: item.NSID, Name: item.Name, Description: item.Description, MembersCount: item.MembersCount, PoolCount: item.PoolCount})
	}
	return r.UpsertGroupsBatch(ctx, values)
}

func (r *Repository) UpsertPersonGroups(ctx context.Context, items []catalog.PersonGroup) error {
	values := make([]PersonGroup, 0, len(items))
	for _, item := range items {
		values = append(values, PersonGroup{PersonNSID: item.PersonNSID, GroupNSID: item.GroupNSID})
	}
	return r.UpsertPersonGroupsBatch(ctx, values)
}
