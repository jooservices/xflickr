package flickr

import "context"

// Gallery is one gallery from flickr.galleries.getList.
type Gallery struct {
	ID             string  `json:"id"`
	GalleryID      string  `json:"gallery_id"`
	URL            string  `json:"url"`
	Owner          string  `json:"owner"`
	Username       string  `json:"username"`
	PrimaryPhotoID string  `json:"primary_photo_id"`
	DateCreate     Int     `json:"date_create"`
	DateUpdate     Int     `json:"date_update"`
	CountPhotos    Int     `json:"count_photos"`
	CountVideos    Int     `json:"count_videos"`
	CountTotal     Int     `json:"count_total"`
	CountComments  Int     `json:"count_comments"`
	Title          Content `json:"title"`
	Description    Content `json:"description"`
}

// GalleriesGetList calls flickr.galleries.getList.
func (c *Client) GalleriesGetList(ctx context.Context, creds Credentials, nsid string, opts ListOptions) (Page[Gallery], error) {
	params := opts.params(false)
	params["user_id"] = nsid
	return listPage[Gallery](ctx, c, creds, "flickr.galleries.getList", "galleries", "gallery", params)
}

// GalleriesGetPhotos calls flickr.galleries.getPhotos.
func (c *Client) GalleriesGetPhotos(ctx context.Context, creds Credentials, galleryID string, opts ListOptions) (Page[Photo], error) {
	params := opts.params(true)
	params["gallery_id"] = galleryID
	return listPage[Photo](ctx, c, creds, "flickr.galleries.getPhotos", "photos", "photo", params)
}
