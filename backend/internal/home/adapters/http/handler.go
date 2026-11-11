// Package http adapts Home snapshot reads to the generated HTTP contract.
package http

import (
	"context"
	"errors"

	"github.com/jooservices/xflickr/internal/home/app"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

type snapshotService interface {
	Get(context.Context, int64) (app.Snapshot, error)
}

// Handler implements GET /me/home for an authenticated user.
type Handler struct {
	Snapshot snapshotService
	ActorID  func(context.Context) (int64, bool)
}

// GetMeHome returns the current user's Home snapshot.
func (h Handler) GetMeHome(ctx context.Context, _ gen.GetMeHomeRequestObject) (gen.GetMeHomeResponseObject, error) {
	if h.ActorID == nil {
		return gen.GetMeHome401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	userID, ok := h.ActorID(ctx)
	if !ok {
		return gen.GetMeHome401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if h.Snapshot == nil {
		return nil, errors.New("home: snapshot service is required")
	}
	snapshot, err := h.Snapshot.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	return gen.GetMeHome200JSONResponse(homeSnapshot(snapshot)), nil
}

func homeSnapshot(snapshot app.Snapshot) gen.HomeSnapshot {
	out := gen.HomeSnapshot{
		GeneratedAt: snapshot.GeneratedAt, CountsComputedAt: snapshot.CountsComputedAt, Estimated: snapshot.Estimated,
		Contacts: gen.HomeContactCounts{
			Total: snapshot.Contacts.Total, Direct: snapshot.Contacts.Direct, Discovered: snapshot.Contacts.Discovered,
		},
		NewPhotos:       make([]gen.HomePhoto, 0, len(snapshot.NewPhotos)),
		StarredContacts: make([]gen.HomeStarredContact, 0, len(snapshot.StarredContacts)),
		Activity:        make([]gen.HomeActivity, 0, len(snapshot.Activity)),
	}
	if snapshot.CountsStale {
		stale := true
		out.CountsStale = &stale
	} else {
		out.Photos = &gen.HomePhotoCounts{
			Total: snapshot.Photos.Total, SizesReady: snapshot.Photos.SizesReady, Captioned: snapshot.Photos.Captioned,
		}
	}
	for _, photo := range snapshot.NewPhotos {
		out.NewPhotos = append(out.NewPhotos, gen.HomePhoto{
			ID: photo.ID, Title: photo.Title, ThumbnailURL: photo.ThumbnailURL,
			OwnerNsid: photo.OwnerNSID, OwnerName: photo.OwnerName, TakenAt: photo.TakenAt,
			Captioned: photo.Captioned,
		})
	}
	for _, contact := range snapshot.StarredContacts {
		item := gen.HomeStarredContact{
			Nsid: contact.NSID, Name: contact.Name, IconURL: contact.IconURL,
			CrawlState: gen.HomeStarredContactCrawlState(contact.CrawlState),
		}
		if !snapshot.CountsStale {
			item.PhotosCount = int64Pointer(contact.PhotosCount)
			item.SizesReady = int64Pointer(contact.SizesReady)
			item.SizesTotal = int64Pointer(contact.SizesTotal)
		}
		out.StarredContacts = append(out.StarredContacts, item)
	}
	for _, item := range snapshot.Activity {
		out.Activity = append(out.Activity, gen.HomeActivity{
			ID: item.ID, Kind: gen.HomeActivityKind(item.Kind), MessageKey: gen.HomeActivityMessageKey(item.MessageKey),
			Count: item.Count, ContactName: item.ContactName, OccurredAt: item.OccurredAt,
			Severity: gen.HomeActivitySeverity(item.Severity),
		})
	}
	return out
}

func int64Pointer(value int64) *int64 { return &value }
