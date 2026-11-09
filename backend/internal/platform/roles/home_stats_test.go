package roles

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/rivertype"

	"github.com/jooservices/xflickr/internal/home/app"
	"github.com/jooservices/xflickr/internal/platform/queue"
)

func TestRegisterHomeStatsRefresh(t *testing.T) {
	adapter, err := queue.NewRiver(&pgxpool.Pool{}, map[string]int{"home": 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adapter.Stop(context.Background()) })

	if err := registerHomeStatsRefresh(adapter, &pgxpool.Pool{}); err != nil {
		t.Fatalf("register home stats refresh: %v", err)
	}
	if err := registerHomeStatsRefresh(adapter, &pgxpool.Pool{}); err == nil {
		t.Fatal("duplicate Home stats refresh handler was accepted")
	}
}

type homeStatsProcessorStub struct {
	done   bool
	err    error
	userID int64
	limit  int
}

func (p *homeStatsProcessorStub) RefreshBatch(_ context.Context, userID int64, limit int) (bool, error) {
	p.userID, p.limit = userID, limit
	return p.done, p.err
}

func TestHomeStatsRefreshHandler(t *testing.T) {
	wantErr := errors.New("refresh failed")
	for _, test := range []struct {
		name      string
		args      []byte
		processor *homeStatsProcessorStub
		wantErr   error
		snooze    bool
	}{
		{name: "invalid job", args: []byte(`{}`), processor: &homeStatsProcessorStub{}, wantErr: app.ErrInvalidUser},
		{name: "incomplete batch", args: []byte(`{"user_id":7}`), processor: &homeStatsProcessorStub{}, snooze: true},
		{name: "complete batch", args: []byte(`{"user_id":7}`), processor: &homeStatsProcessorStub{done: true}},
		{name: "processor failure", args: []byte(`{"user_id":7}`), processor: &homeStatsProcessorStub{err: wantErr}, wantErr: wantErr},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := homeStatsRefreshHandler(app.Refresher{Processor: test.processor, BatchSize: app.RefreshBatchSize})
			err := handler(t.Context(), queue.Job{Args: test.args})
			if test.snooze {
				var snooze *rivertype.JobSnoozeError
				if !errors.As(err, &snooze) || snooze.Duration != time.Second {
					t.Fatalf("incomplete refresh error = %v, want one-second snooze", err)
				}
			} else if !errors.Is(err, test.wantErr) {
				t.Fatalf("refresh error = %v, want %v", err, test.wantErr)
			}
			if test.name != "invalid job" && (test.processor.userID != 7 || test.processor.limit != app.RefreshBatchSize) {
				t.Fatalf("refresh call = user %d, limit %d", test.processor.userID, test.processor.limit)
			}
		})
	}
}
