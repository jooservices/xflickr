package app

import (
	"context"
	"errors"
	"time"

	"github.com/jooservices/xflickr/pkg/flickr"
)

// EntityRef identifies the resource addressed by a Flickr method, not records
// returned by that method (a person's albums list addresses the person).
type EntityRef struct{ Kind, ID string }

// EntityAccess scopes a facet restriction to the credentials used for the call.
// Version is the photo version read before the call; zero for other entities.
type EntityAccess struct {
	EntityRef
	Facet         string
	UserID, KeyID int64
	AccountID     int64
	Version       time.Time
}

// EntityStates persists global disappearance and caller-specific restrictions.
type EntityStates interface {
	Suppressed(context.Context, EntityAccess, time.Time) (bool, error)
	RecordFailure(context.Context, EntityAccess, string, string, time.Time, time.Time) error
}

// AddressedEntity resolves the ID actually sent to Flickr. Unknown or implicit
// subjects are left alone rather than marking an unrelated catalog record.
func AddressedEntity(r Request) (EntityRef, bool) {
	for _, p := range []struct{ param, kind string }{{"photo_id", "photo"}, {"photoset_id", "album"}, {"gallery_id", "gallery"}, {"group_id", "group"}, {"user_id", "person"}} {
		if id := r.Params[p.param]; id != "" {
			return EntityRef{Kind: p.kind, ID: id}, true
		}
	}
	return EntityRef{}, false
}

// FailureState distinguishes a missing entity from a closed facet. Photos are
// declared gone only by reconciliation, never by a photo-addressed method.
func FailureState(ref EntityRef, err error) string {
	if errors.Is(err, flickr.ErrPermission) || errors.Is(err, flickr.ErrResourceDenied) {
		return "restricted"
	}
	if errors.Is(err, flickr.ErrNotFound) {
		if ref.Kind == "photo" {
			return "restricted"
		}
		return "gone"
	}
	return ""
}

func recordEntityFailure(ctx context.Context, states EntityStates, access EntityAccess, cause error, now time.Time, interval time.Duration) error {
	if states == nil {
		return nil
	}
	state := FailureState(access.EntityRef, cause)
	if state == "" {
		return nil
	}
	// Persist stable classifications, never upstream messages or request secrets.
	reason := "not_found"
	if !errors.Is(cause, flickr.ErrNotFound) {
		reason = "permission_denied"
	}
	return states.RecordFailure(ctx, access, state, reason, now, now.Add(interval))
}

func entitySuppressed(ctx context.Context, states EntityStates, a EntityAccess, now time.Time) (bool, error) {
	if states == nil {
		return false, nil
	}
	return states.Suppressed(ctx, a, now)
}
