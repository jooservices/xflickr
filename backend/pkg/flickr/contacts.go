package flickr

import "context"

// Contact is one entry of a contact list.
type Contact struct {
	NSID       string `json:"nsid"`
	Username   string `json:"username"`
	RealName   string `json:"realname"`
	Location   string `json:"location"`
	PathAlias  string `json:"path_alias"`
	IconServer String `json:"iconserver"`
	IconFarm   Int    `json:"iconfarm"`
	Ignored    Int    `json:"ignored"`
	Friend     Int    `json:"friend"`
	Family     Int    `json:"family"`
}

// ContactsGetList calls flickr.contacts.getList for the authenticated member.
func (c *Client) ContactsGetList(ctx context.Context, creds Credentials, opts ListOptions) (Page[Contact], error) {
	return listPage[Contact](ctx, c, creds, "flickr.contacts.getList", "contacts", "contact", opts.params(false))
}

// ContactsGetPublicList calls flickr.contacts.getPublicList.
func (c *Client) ContactsGetPublicList(ctx context.Context, creds Credentials, nsid string, opts ListOptions) (Page[Contact], error) {
	params := opts.params(false)
	params["user_id"] = nsid
	return listPage[Contact](ctx, c, creds, "flickr.contacts.getPublicList", "contacts", "contact", params)
}
