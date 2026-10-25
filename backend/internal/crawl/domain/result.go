package domain

import (
	"encoding/json"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
)

// Observation is a fact noticed while handling a page, such as a changed
// photo or a missing person. It is stored with the page's commit.
type Observation struct {
	Kind    string
	Subject string
	Data    json.RawMessage
}

// Discovery is a person found on a page. The engine records an interest one
// level deeper than the item that found them, and plans their work.
type Discovery struct {
	NSID string
	Via  string
}

// Result is what a facet extracted from one page. Everything in it is
// committed together with the cursor advance.
type Result struct {
	People       []catalog.Person
	Photos       []catalog.Photo
	Observations []Observation
	Discoveries  []Discovery
	Work         []WorkSpec
	// NextCursor changes the next request without finishing the same work item.
	NextCursor *Cursor
	// HasMore asks for the next page of the same item; false finishes it.
	HasMore bool
}
