package roles

import (
	"context"
	"time"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db"
	scheduleriver "github.com/jooservices/xflickr/internal/schedule/adapters/river"
)

// RequeueFailedWork returns failed work items (all when ids is empty) to
// pending so the normal pump and quota gate dispatch them again. It reports
// how many rows were requeued.
func RequeueFailedWork(ctx context.Context, ids []int64) (int, error) {
	dbConfig, err := config.LoadDatabase()
	if err != nil {
		return 0, err
	}
	pool, err := db.NewPool(ctx, dbConfig)
	if err != nil {
		return 0, err
	}
	defer pool.Close()
	return scheduleriver.Store{Pool: pool}.Requeue(ctx, ids, time.Now())
}
