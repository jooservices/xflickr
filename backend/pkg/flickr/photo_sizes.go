package flickr

import (
	"context"
	"errors"
)

// PhotoSize is one available size returned by flickr.photos.getSizes.
type PhotoSize struct {
	Label  string `json:"label"`
	Width  Int    `json:"width"`
	Height Int    `json:"height"`
	Source string `json:"source"`
	Media  string `json:"media"`
}

// LargestAllowed chooses Flickr's Original size when present, otherwise the
// size with the greatest pixel area. It returns the zero value for no sizes.
func LargestAllowed(sizes []PhotoSize) PhotoSize {
	if len(sizes) == 0 {
		return PhotoSize{}
	}
	largest := sizes[0]
	for _, size := range sizes {
		if size.Label == "Original" {
			return size
		}
		if int64(size.Width)*int64(size.Height) > int64(largest.Width)*int64(largest.Height) {
			largest = size
		}
	}
	return largest
}

// PhotosGetSizes calls flickr.photos.getSizes.
func (c *Client) PhotosGetSizes(ctx context.Context, creds Credentials, photoID string) ([]PhotoSize, error) {
	page, err := listPage[PhotoSize](ctx, c, creds, "flickr.photos.getSizes", "sizes", "size", map[string]string{"photo_id": photoID})
	var callErr *CallError
	if errors.As(err, &callErr) && callErr.Code == 2 {
		// Flickr uses code 2 for permission denied on photos.getSizes. Other
		// methods use code 2 for different errors, so keep this correction local.
		permissionErr := *callErr
		permissionErr.Kind = ErrPermission
		return page.Items, &permissionErr
	}
	return page.Items, err
}
