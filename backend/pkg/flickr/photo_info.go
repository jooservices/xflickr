package flickr

import "context"

// PhotoInfo is the detailed record returned by flickr.photos.getInfo.
type PhotoInfo struct {
	ID             string          `json:"id"`
	Secret         string          `json:"secret"`
	Server         String          `json:"server"`
	Farm           Int             `json:"farm"`
	DateUploaded   String          `json:"dateuploaded"`
	IsFavorite     Int             `json:"isfavorite"`
	License        String          `json:"license"`
	SafetyLevel    Int             `json:"safety_level"`
	Rotation       Int             `json:"rotation"`
	OriginalSecret string          `json:"originalsecret"`
	OriginalFormat string          `json:"originalformat"`
	Owner          PhotoInfoOwner  `json:"owner"`
	Title          Content         `json:"title"`
	Description    Content         `json:"description"`
	Visibility     PhotoVisibility `json:"visibility"`
	Dates          PhotoDates      `json:"dates"`
	Tags           PhotoTags       `json:"tags"`
	URLs           PhotoURLs       `json:"urls"`
}

// PhotoInfoOwner identifies the owner nested in a photo info response.
type PhotoInfoOwner struct {
	NSID       string `json:"nsid"`
	Username   string `json:"username"`
	RealName   string `json:"realname"`
	Location   string `json:"location"`
	IconServer String `json:"iconserver"`
	IconFarm   Int    `json:"iconfarm"`
	PathAlias  string `json:"path_alias"`
}

// PhotoVisibility contains Flickr's photo access flags.
type PhotoVisibility struct {
	IsPublic Int `json:"ispublic"`
	IsFriend Int `json:"isfriend"`
	IsFamily Int `json:"isfamily"`
}

// PhotoDates contains upload and taken dates.
type PhotoDates struct {
	Posted       String `json:"posted"`
	Taken        String `json:"taken"`
	TakenGran    Int    `json:"takengranularity"`
	TakenUnknown Int    `json:"takenunknown"`
}

// PhotoTag is one tag attached to a photo info response.
type PhotoTag struct {
	ID         string `json:"id"`
	Author     string `json:"author"`
	AuthorName string `json:"authorname"`
	Raw        string `json:"raw"`
	Text       string `json:"_content"`
	MachineTag Int    `json:"machine_tag"`
}

// PhotoTags is the tag collection nested in a photo info response.
type PhotoTags struct {
	Items []PhotoTag `json:"tag"`
}

// PhotoURL is a link associated with a photo.
type PhotoURL struct {
	Type string  `json:"type"`
	URL  Content `json:"_content"`
}

// PhotoURLs is the URL collection nested in a photo info response.
type PhotoURLs struct {
	Items []PhotoURL `json:"url"`
}

// PhotosGetInfo calls flickr.photos.getInfo. Secret may be empty for a public
// photo; Flickr requires it for some private photo lookups.
func (c *Client) PhotosGetInfo(ctx context.Context, creds Credentials, photoID, secret string) (PhotoInfo, error) {
	params := map[string]string{"photo_id": photoID}
	if secret != "" {
		params["secret"] = secret
	}
	return getObject[PhotoInfo](ctx, c, creds, "flickr.photos.getInfo", "photo", params)
}
