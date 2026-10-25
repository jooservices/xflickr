package app

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrControlRepositoryRequired = errors.New("crawl: control repository is required")
	ErrRescorerRequired          = errors.New("crawl: backlog rescorer is required")
	ErrInvalidPerson             = errors.New("crawl: person NSID is required")
	ErrPersonNotFound            = errors.New("crawl: person interest not found")
)

type DepthCount struct {
	Depth    int
	Contacts int64
}
type FacetProgress struct {
	Facet  string
	Total  int64
	Done   int64
	Active int64
	Failed int64
}
type LaneStatus struct {
	Lane    string
	Pending int64
	Running int64
	Waiting int64
	Paused  int64
}
type CrawlStatus struct {
	Depths []DepthCount
	Facets []FacetProgress
	Lanes  []LaneStatus
}

// ControlRepository reads status and updates a user's contact controls.
type ControlRepository interface {
	Status(context.Context, int64) (CrawlStatus, error)
	SetStarred(context.Context, int64, string, bool) error
	Select(context.Context, int64, string, time.Time) error
	SetPaused(context.Context, int64, string, bool) error
}

// Rescorer refreshes the stored priority of a person's pending backlog.
type Rescorer interface {
	RescorePerson(context.Context, int64, string) error
}

// Controls exposes crawl progress and contact-level controls.
type Controls struct {
	Repository ControlRepository
	Rescorer   Rescorer
	Now        func() time.Time
}

func (service Controls) Status(ctx context.Context, userID int64) (CrawlStatus, error) {
	if err := service.ready(ctx, userID, ""); err != nil {
		return CrawlStatus{}, err
	}
	return service.Repository.Status(ctx, userID)
}

func (service Controls) Star(ctx context.Context, userID int64, person string, starred bool) error {
	if err := service.ready(ctx, userID, person); err != nil {
		return err
	}
	person = strings.TrimSpace(person)
	if err := service.Repository.SetStarred(ctx, userID, person, starred); err != nil {
		return err
	}
	return service.Rescorer.RescorePerson(ctx, userID, person)
}

func (service Controls) Select(ctx context.Context, userID int64, person string) error {
	if err := service.ready(ctx, userID, person); err != nil {
		return err
	}
	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}
	person = strings.TrimSpace(person)
	if err := service.Repository.Select(ctx, userID, person, now.Add(7*24*time.Hour)); err != nil {
		return err
	}
	return service.Rescorer.RescorePerson(ctx, userID, person)
}

func (service Controls) Pause(ctx context.Context, userID int64, person string) error {
	if err := service.ready(ctx, userID, person); err != nil {
		return err
	}
	return service.Repository.SetPaused(ctx, userID, strings.TrimSpace(person), true)
}

func (service Controls) Resume(ctx context.Context, userID int64, person string) error {
	if err := service.ready(ctx, userID, person); err != nil {
		return err
	}
	return service.Repository.SetPaused(ctx, userID, strings.TrimSpace(person), false)
}

func (service Controls) ready(ctx context.Context, userID int64, person string) error {
	if service.Repository == nil {
		return ErrControlRepositoryRequired
	}
	if ctx == nil {
		return ErrContextRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if userID <= 0 {
		return errors.New("crawl: user id must be positive")
	}
	if person != "" && strings.TrimSpace(person) == "" {
		return ErrInvalidPerson
	}
	if service.Rescorer == nil {
		return ErrRescorerRequired
	}
	return nil
}
