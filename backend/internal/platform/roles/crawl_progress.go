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
