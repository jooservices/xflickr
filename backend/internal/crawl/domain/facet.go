package domain

import (
	"context"
	"errors"
	"strings"
)

// Lane is the scheduling lane a facet's Flickr calls belong to.
type Lane string

const (
	// LanePerson covers contacts, photos, favorites, albums and related records.
	LanePerson Lane = "person"
	// LanePhoto covers photo size retrieval.
	LanePhoto Lane = "photo"
)

// ErrInvalidFacet means a facet cannot be registered.
var ErrInvalidFacet = errors.New("crawl: invalid facet")

// Valid reports whether l names a supported lane.
func (l Lane) Valid() bool { return l == LanePerson || l == LanePhoto }

// Person is the subject a facet plans work for, as seen by one user.
type Person struct {
	NSID   string
	UserID int64
	Depth  int
}

// WorkSpec describes one unit of crawl work: one Flickr method that is paged
// until a page reports no more results. Kind names the facet that handles it;
// empty means the facet that produced the spec.
type WorkSpec struct {
	Kind         string
	Subject      string
	Method       string
	Params       map[string]string
	CheckpointAt int64
}

// Validate checks that the spec can be stored as a work item.
func (s WorkSpec) Validate() error {
	if strings.TrimSpace(s.Subject) == "" || strings.TrimSpace(s.Method) == "" {
		return ErrInvalidFacet
	}
	return nil
}

// Facet is one kind of per-person crawl, such as contacts or photos. All
// facets share the engine; a new facet is one implementation of this interface.
type Facet interface {
	// Name is the work item kind the facet handles.
	Name() string
	// Lane is the quota lane the facet's calls are charged to.
	Lane() Lane
	// Plan lists the work to create for a person, or nothing when the facet
	// does not apply.
	Plan(ctx context.Context, person Person) []WorkSpec
	// Handle turns one fetched page into the records, discoveries and
	// follow-up work it yields.
	Handle(ctx context.Context, page Page) (Result, error)
}
