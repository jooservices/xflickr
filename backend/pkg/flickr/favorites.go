package flickr

import "context"

// FavoritesGetList calls flickr.favorites.getList. An empty nsid lists the
// authenticated member's favorites.
func (c *Client) FavoritesGetList(ctx context.Context, creds Credentials, nsid string, opts ListOptions) (Page[Photo], error) {
	params := opts.params(true)
	if nsid != "" {
		params["user_id"] = nsid
	}
	return listPage[Photo](ctx, c, creds, "flickr.favorites.getList", "photos", "photo", params)
}

// FavoritesGetPublicList calls flickr.favorites.getPublicList.
func (c *Client) FavoritesGetPublicList(ctx context.Context, creds Credentials, nsid string, opts ListOptions) (Page[Photo], error) {
	params := opts.params(true)
	params["user_id"] = nsid
	return listPage[Photo](ctx, c, creds, "flickr.favorites.getPublicList", "photos", "photo", params)
}
