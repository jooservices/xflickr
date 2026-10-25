package flickr

import (
	"context"
	"strconv"
)

// Photo is one item of a photo list. Fields beyond the identifiers are present
// only when requested through extras.
type Photo struct {
	ID          string  `json:"id"`
	Owner       string  `json:"owner"`
	Secret      string  `json:"secret"`
	Server      String  `json:"server"`
	Farm        Int     `json:"farm"`
	Title       string  `json:"title"`
	IsPublic    Int     `json:"ispublic"`
	IsFriend    Int     `json:"isfriend"`
	IsFamily    Int     `json:"isfamily"`
	OwnerName   string  `json:"ownername"`
	DateUpload  Int     `json:"dateupload"`
	LastUpdate  Int     `json:"lastupdate"`
	DateFaved   Int     `json:"date_faved"`
	Views       Int     `json:"views"`
	Tags        string  `json:"tags"`
	License     String  `json:"license"`
	Media       string  `json:"media"`
	Description Content `json:"description"`
	OWidth      Int     `json:"o_width"`
	OHeight     Int     `json:"o_height"`
	URLSq       string  `json:"url_sq"`
	URLT        string  `json:"url_t"`
	URLS        string  `json:"url_s"`
	URLM        string  `json:"url_m"`
	URLZ        string  `json:"url_z"`
	URLL        string  `json:"url_l"`
	URLO        string  `json:"url_o"`
	WidthO      Int     `json:"width_o"`
	HeightO     Int     `json:"height_o"`
}

// PeopleGetPhotos calls flickr.people.getPhotos.
func (c *Client) PeopleGetPhotos(ctx context.Context, creds Credentials, nsid string, opts ListOptions) (Page[Photo], error) {
	params := opts.params(true)
	params["user_id"] = nsid
	return listPage[Photo](ctx, c, creds, "flickr.people.getPhotos", "photos", "photo", params)
}

// PeopleGetPublicPhotos calls flickr.people.getPublicPhotos.
func (c *Client) PeopleGetPublicPhotos(ctx context.Context, creds Credentials, nsid string, opts ListOptions) (Page[Photo], error) {
	params := opts.params(true)
	params["user_id"] = nsid
	return listPage[Photo](ctx, c, creds, "flickr.people.getPublicPhotos", "photos", "photo", params)
}

// ContactsPhotosOptions are the parameters of flickr.photos.getContactsPhotos,
// which is not paginated.
type ContactsPhotosOptions struct {
	Extras      string
	Count       int
	JustFriends bool
	SinglePhoto bool
	IncludeSelf bool
}

// PhotosGetContactsPhotos calls flickr.photos.getContactsPhotos.
func (c *Client) PhotosGetContactsPhotos(ctx context.Context, creds Credentials, opts ContactsPhotosOptions) ([]Photo, error) {
	params := ListOptions{Extras: opts.Extras}.params(true)
	if opts.Count > 0 {
		params["count"] = strconv.Itoa(opts.Count)
	}
	for key, on := range map[string]bool{"just_friends": opts.JustFriends, "single_photo": opts.SinglePhoto, "include_self": opts.IncludeSelf} {
		if on {
			params[key] = "1"
		}
	}
	page, err := listPage[Photo](ctx, c, creds, "flickr.photos.getContactsPhotos", "photos", "photo", params)
	return page.Items, err
}
