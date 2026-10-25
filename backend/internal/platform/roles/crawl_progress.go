package roles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	crawlhttp "github.com/jooservices/xflickr/internal/crawl/adapters/http"
	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	platformsse "github.com/jooservices/xflickr/internal/platform/sse"
)

// crawlProgressNotifier publishes a user's crawl status as a crawl.progress
// event whenever the crawl engine reports that the user's crawl state changed.
type crawlProgressNotifier struct {
	Status crawlStatusReader
	Events downloadEventPublisher
}

type crawlStatusReader interface {
	Status(context.Context, int64) (crawlapp.CrawlStatus, error)
}

var _ crawlapp.ProgressNotifier = crawlProgressNotifier{}

func (n crawlProgressNotifier) CrawlChanged(ctx context.Context, userID int64) error {
	if n.Status == nil || n.Events == nil {
		return errors.New("crawl: progress notifier dependencies are required")
	}
	status, err := n.Status.Status(ctx, userID)
	if err != nil {
		return fmt.Errorf("crawl: load progress for event: %w", err)
	}
	data, err := json.Marshal(crawlhttp.StatusResponse(status))
	if err != nil {
		return fmt.Errorf("crawl: encode progress event: %w", err)
	}
	return n.Events.Publish(ctx, platformsse.Event{UserID: userID, Type: "crawl.progress", Data: data})
}

// publishingControls publishes crawl.progress after every successful crawl
// control change (star, select, pause, resume) made through the API.
type publishingControls struct {
	crawlhttp.ControlService
	Notifier crawlapp.ProgressNotifier
}

func (c publishingControls) Star(ctx context.Context, user int64, nsid string, starred bool) error {
	return c.notify(ctx, user, c.ControlService.Star(ctx, user, nsid, starred))
}

func (c publishingControls) Select(ctx context.Context, user int64, nsid string) error {
	return c.notify(ctx, user, c.ControlService.Select(ctx, user, nsid))
}

func (c publishingControls) Pause(ctx context.Context, user int64, nsid string) error {
	return c.notify(ctx, user, c.ControlService.Pause(ctx, user, nsid))
}

func (c publishingControls) Resume(ctx context.Context, user int64, nsid string) error {
	return c.notify(ctx, user, c.ControlService.Resume(ctx, user, nsid))
}

// notify never fails the control change: the event is best-effort and the
// next status read still returns the committed state.
func (c publishingControls) notify(ctx context.Context, user int64, err error) error {
	if err == nil && c.Notifier != nil {
		_ = c.Notifier.CrawlChanged(ctx, user)
	}
	return err
}
