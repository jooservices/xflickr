// Package domain contains catalog records and their business rules.
package domain

import "time"

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
