package flickr

import "context"

// Group is a group membership entry from flickr.people.getGroups and
// flickr.people.getPublicGroups.
type Group struct {
	NSID           string `json:"nsid"`
	Name           string `json:"name"`
	Admin          Int    `json:"admin"`
	EighteenPlus   Int    `json:"eighteenplus"`
	InvitationOnly Int    `json:"invitation_only"`
	Members        Int    `json:"members"`
	PoolCount      Int    `json:"pool_count"`
}

// GroupInfo is the result of flickr.groups.getInfo.
type GroupInfo struct {
	ID          string  `json:"id"`
	IconServer  String  `json:"iconserver"`
	IconFarm    Int     `json:"iconfarm"`
	Name        Content `json:"name"`
	Description Content `json:"description"`
	Members     Content `json:"members"`
	PoolCount   Content `json:"pool_count"`
	Privacy     Content `json:"privacy"`
	Rules       Content `json:"rules"`
}

// PeopleGetGroups calls flickr.people.getGroups.
func (c *Client) PeopleGetGroups(ctx context.Context, creds Credentials, nsid string) ([]Group, error) {
	return c.groups(ctx, creds, "flickr.people.getGroups", nsid)
}

// PeopleGetPublicGroups calls flickr.people.getPublicGroups.
func (c *Client) PeopleGetPublicGroups(ctx context.Context, creds Credentials, nsid string) ([]Group, error) {
	return c.groups(ctx, creds, "flickr.people.getPublicGroups", nsid)
}

func (c *Client) groups(ctx context.Context, creds Credentials, method, nsid string) ([]Group, error) {
	page, err := listPage[Group](ctx, c, creds, method, "groups", "group", map[string]string{"user_id": nsid})
	return page.Items, err
}

// GroupsGetInfo calls flickr.groups.getInfo.
func (c *Client) GroupsGetInfo(ctx context.Context, creds Credentials, groupID string) (GroupInfo, error) {
	return getObject[GroupInfo](ctx, c, creds, "flickr.groups.getInfo", "group", map[string]string{"group_id": groupID})
}

// GroupsPoolsGetPhotos calls flickr.groups.pools.getPhotos.
func (c *Client) GroupsPoolsGetPhotos(ctx context.Context, creds Credentials, groupID string, opts ListOptions) (Page[Photo], error) {
	params := opts.params(true)
	params["group_id"] = groupID
	return listPage[Photo](ctx, c, creds, "flickr.groups.pools.getPhotos", "photos", "photo", params)
}
