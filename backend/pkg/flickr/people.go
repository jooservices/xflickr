package flickr

import "context"

// Person is the result of flickr.people.getInfo.
type Person struct {
	ID          string  `json:"id"`
	NSID        string  `json:"nsid"`
	IsPro       Int     `json:"ispro"`
	IsDeleted   Int     `json:"is_deleted"`
	IconServer  String  `json:"iconserver"`
	IconFarm    Int     `json:"iconfarm"`
	PathAlias   string  `json:"path_alias"`
	HasStats    Int     `json:"has_stats"`
	Username    Content `json:"username"`
	RealName    Content `json:"realname"`
	Location    Content `json:"location"`
	Description Content `json:"description"`
	PhotosURL   Content `json:"photosurl"`
	ProfileURL  Content `json:"profileurl"`
	MobileURL   Content `json:"mobileurl"`
	Photos      struct {
		FirstDate      Content `json:"firstdate"`
		FirstDateTaken Content `json:"firstdatetaken"`
		Count          Content `json:"count"`
	} `json:"photos"`
}

// PeopleGetInfo calls flickr.people.getInfo.
func (c *Client) PeopleGetInfo(ctx context.Context, creds Credentials, nsid string) (Person, error) {
	return getObject[Person](ctx, c, creds, "flickr.people.getInfo", "person", map[string]string{"user_id": nsid})
}

// TestLogin calls flickr.test.login and returns the authenticated user.
func (c *Client) TestLogin(ctx context.Context, creds Credentials) (LoginUser, error) {
	return getObject[LoginUser](ctx, c, creds, "flickr.test.login", "user", nil)
}

// LoginUser is the authenticated member reported by flickr.test.login.
type LoginUser struct {
	ID       string  `json:"id"`
	Username Content `json:"username"`
}

// TestEcho calls flickr.test.echo and returns the echoed parameters.
func (c *Client) TestEcho(ctx context.Context, creds Credentials, params map[string]string) (map[string]string, error) {
	raw, err := c.call(ctx, creds, "flickr.test.echo", params)
	if err != nil {
		return nil, err
	}
	var fields map[string]Content
	if err := decodeObject(raw, &fields); err != nil {
		return nil, err
	}
	echoed := make(map[string]string, len(fields))
	for key, value := range fields {
		if key == "stat" {
			continue
		}
		echoed[key] = string(value)
	}
	return echoed, nil
}
