package roles

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/riverqueue/river/rivertype"

	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/pkg/flickr"
)

func TestSnoozeOnQuotaSnoozesInsteadOfFailing(t *testing.T) {
	waiting := fmt.Errorf("get sizes: %w", crawlapp.NewQuotaWaitError(flickr.ErrQuotaExceeded, 90*time.Second))
	var snooze *rivertype.JobSnoozeError
	if err := snoozeOnQuota(waiting); !errors.As(err, &snooze) || snooze.Duration != 90*time.Second {
		t.Fatalf("snoozeOnQuota(quota) = %v", err)
	}
	other := errors.New("boom")
	if err := snoozeOnQuota(other); !errors.Is(err, other) {
		t.Fatalf("snoozeOnQuota(other) = %v", err)
	}
	if err := snoozeOnQuota(nil); err != nil {
		t.Fatalf("snoozeOnQuota(nil) = %v", err)
	}
}
