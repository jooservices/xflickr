// Package domain contains catalog records and their business rules.
package domain

import "time"

// Favorite is a Flickr favorite relationship.
type Favorite struct {
	PersonNSID string
	PhotoID    int64
	FaveDate   time.Time
}

// Album is Flickr photoset metadata.
type Album struct {
	ID, OwnerNSID, Title, Description string
	PrimaryPhotoID                    *int64
	PhotosCount                       int64
	CreatedAt, UpdatedAt              time.Time
}

// Gallery is Flickr gallery metadata.
type Gallery struct {
	ID, OwnerNSID, Title, Description string
	PrimaryPhotoID                    *int64
	PhotosCount                       int64
	CreatedAt, UpdatedAt              time.Time
}

// Group is Flickr group metadata.
type Group struct {
	NSID, Name, Description string
	MembersCount, PoolCount int64
}

// PersonGroup is a discovered group membership.
type PersonGroup struct{ PersonNSID, GroupNSID string }

// Person is a Flickr person record stored in the catalog.
type Person struct {
	NSID        string
	Username    string
	Realname    string
	Profile     []byte
	Icon        string
	PhotosCount int64
	LastSeenAt  *time.Time
	FacetState  []byte
}
