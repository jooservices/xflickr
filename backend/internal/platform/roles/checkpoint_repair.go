package roles

import (
	"context"

	crawlpostgres "github.com/jooservices/xflickr/internal/crawl/adapters/postgres"
	"github.com/jooservices/xflickr/internal/platform/config"
)

// CheckpointRepairOptions bounds an offline checkpoint repair run.
type CheckpointRepairOptions = crawlpostgres.CheckpointRepairOptions

// CheckpointRepairReport includes counts even when nothing changed.
type CheckpointRepairReport = crawlpostgres.CheckpointRepairReport

// repairZeroCheckpoints is the pool-backed repair used after opening the
// configured database. Unit tests replace it so the CLI/job entry can be
// exercised without a live PostgreSQL.
var repairZeroCheckpoints = crawlpostgres.RepairZeroCheckpointsPool

// RepairZeroCheckpoints fills null/zero interest checkpoints from stored
// catalog data. It makes no Flickr calls and never starts application roles.
func RepairZeroCheckpoints(ctx context.Context, options CheckpointRepairOptions) (CheckpointRepairReport, error) {
	cfg, err := config.LoadDatabase()
	if err != nil {
		return CheckpointRepairReport{}, err
	}
	pool, err := openRoleDatabase(ctx, cfg)
	if err != nil {
		return CheckpointRepairReport{}, err
	}
	defer pool.Close()
	return repairZeroCheckpoints(ctx, pool, options)
}
