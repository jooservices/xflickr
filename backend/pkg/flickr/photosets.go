package flickr

import "context"

// Photoset is one album from flickr.photosets.getList.
type Photoset struct {
	ID          string  `json:"id"`
	Owner       string  `json:"owner"`
	Username    string  `json:"username"`
	Primary     string  `json:"primary"`
	Secret      string  `json:"secret"`
	Server      String  `json:"server"`
	Farm        Int     `json:"farm"`
	Photos      Int     `json:"photos"`
	Videos      Int     `json:"videos"`
	CountViews  Int     `json:"count_views"`
	DateCreate  Int     `json:"date_create"`
	DateUpdate  Int     `json:"date_update"`
	Title       Content `json:"title"`
	Description Content `json:"description"`
}

// PhotosetsGetList calls flickr.photosets.getList. An empty nsid lists the
// authenticated member's albums.
func (c *Client) PhotosetsGetList(ctx context.Context, creds Credentials, nsid string, opts ListOptions) (Page[Photoset], error) {
	params := opts.params(false)
	if nsid != "" {
		params["user_id"] = nsid
	}
	return listPage[Photoset](ctx, c, creds, "flickr.photosets.getList", "photosets", "photoset", params)
}

// PhotosetsGetPhotos calls flickr.photosets.getPhotos.
func (c *Client) PhotosetsGetPhotos(ctx context.Context, creds Credentials, nsid, photosetID string, opts ListOptions) (Page[Photo], error) {
	params := opts.params(true)
	params["photoset_id"] = photosetID
	if nsid != "" {
		params["user_id"] = nsid
	}
	return listPage[Photo](ctx, c, creds, "flickr.photosets.getPhotos", "photoset", "photo", params)
}
