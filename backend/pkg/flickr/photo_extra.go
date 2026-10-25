package flickr

import "context"

// PhotoExif is the camera and EXIF metadata returned for a photo.
type PhotoExif struct {
	ID      string      `json:"id"`
	Secret  string      `json:"secret"`
	Server  String      `json:"server"`
	Farm    Int         `json:"farm"`
	Camera  string      `json:"camera"`
	Entries []ExifEntry `json:"exif"`
}

// ExifEntry is one metadata field in flickr.photos.getExif.
type ExifEntry struct {
	Tagspace   string  `json:"tagspace"`
	TagspaceID Int     `json:"tagspaceid"`
	Tag        string  `json:"tag"`
	Label      string  `json:"label"`
	Raw        Content `json:"raw"`
	Clean      Content `json:"clean"`
}

// PhotoContexts contains the albums and groups containing a photo.
type PhotoContexts struct {
	Sets  []PhotoContext `json:"set"`
	Pools []PhotoContext `json:"pool"`
}

// PhotoContext describes one photoset or group pool context.
type PhotoContext struct {
	ID    String  `json:"id"`
	Title Content `json:"title"`
	URL   Content `json:"url"`
}

// PhotoComment is one comment on a photo.
type PhotoComment struct {
	ID         string  `json:"id"`
	Author     string  `json:"author"`
	AuthorName string  `json:"authorname"`
	DateCreate String  `json:"datecreate"`
	Permalink  string  `json:"permalink"`
	Text       Content `json:"_content"`
}

// PhotoFavorite is one person who favorited a photo.
type PhotoFavorite struct {
	NSID       string `json:"nsid"`
	Username   string `json:"username"`
	RealName   string `json:"realname"`
	FaveDate   String `json:"favedate"`
	IconServer String `json:"iconserver"`
	IconFarm   Int    `json:"iconfarm"`
}

// PhotoPerson is one person tagged in a photo.
type PhotoPerson struct {
	NSID        string `json:"nsid"`
	Username    string `json:"username"`
	RealName    string `json:"realname"`
	IsPro       Int    `json:"ispro"`
	CanSeePhoto Int    `json:"canseephoto"`
	PhotoID     string `json:"photo_id"`
}

// PhotosGetExif calls flickr.photos.getExif.
func (c *Client) PhotosGetExif(ctx context.Context, creds Credentials, photoID, secret string) (PhotoExif, error) {
	params := map[string]string{"photo_id": photoID}
	if secret != "" {
		params["secret"] = secret
	}
	return getObject[PhotoExif](ctx, c, creds, "flickr.photos.getExif", "photo", params)
}

// PhotosGetAllContexts calls flickr.photos.getAllContexts.
func (c *Client) PhotosGetAllContexts(ctx context.Context, creds Credentials, photoID string) (PhotoContexts, error) {
	var contexts PhotoContexts
	raw, err := c.call(ctx, creds, "flickr.photos.getAllContexts", map[string]string{"photo_id": photoID})
	if err != nil {
		return contexts, err
	}
	if err := decodeObject(raw, &contexts); err != nil {
		return PhotoContexts{}, err
	}
	return contexts, nil
}

// PhotosCommentsGetList calls flickr.photos.comments.getList.
func (c *Client) PhotosCommentsGetList(ctx context.Context, creds Credentials, photoID string, opts ListOptions) (Page[PhotoComment], error) {
	params := opts.params(false)
	params["photo_id"] = photoID
	return listPage[PhotoComment](ctx, c, creds, "flickr.photos.comments.getList", "comments", "comment", params)
}

// PhotosGetFavorites calls flickr.photos.getFavorites.
func (c *Client) PhotosGetFavorites(ctx context.Context, creds Credentials, photoID string, opts ListOptions) (Page[PhotoFavorite], error) {
	params := opts.params(false)
	params["photo_id"] = photoID
	return listPage[PhotoFavorite](ctx, c, creds, "flickr.photos.getFavorites", "photo", "person", params)
}

// PhotosPeopleGetList calls flickr.photos.people.getList.
func (c *Client) PhotosPeopleGetList(ctx context.Context, creds Credentials, photoID string) ([]PhotoPerson, error) {
	page, err := listPage[PhotoPerson](ctx, c, creds, "flickr.photos.people.getList", "people", "person", map[string]string{"photo_id": photoID})
	return page.Items, err
}
