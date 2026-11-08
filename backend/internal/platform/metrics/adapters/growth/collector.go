// Package growth samples approximate data-store sizes without scanning large tables.
package growth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

// Querier is the PostgreSQL surface used for size estimates.
type Querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// RedisStats reads optional Redis memory metrics.
type RedisStats interface {
	UsedMemory(ctx context.Context) (uint64, error)
	KeyCount(ctx context.Context) (int64, error)
}

// SearchStats reads optional Elasticsearch/OpenSearch index metrics.
type SearchStats interface {
	DocumentCount(ctx context.Context) (int64, error)
	StoreBytes(ctx context.Context) (int64, error)
}

// DiskUsage reports used and free bytes for a media path.
type DiskUsage interface {
	Usage(path string) (used, free uint64, err error)
}

// Collector samples database, cache, search and media growth.
type Collector struct {
	Pool       Querier
	Clock      domain.Clock
	Redis      RedisStats
	Search     SearchStats
	Disk       DiskUsage
	MediaPath  string
	MainTables []string
	TopN       int
}

var _ domain.Collector = (*Collector)(nil)

var defaultTables = []string{
	"people", "photos", "albums", "galleries", "groups", "favorites",
	"observations", "api_call_log", "work_items", "outbox_events",
}

// NewCollector validates required dependencies.
func NewCollector(pool Querier, clock domain.Clock) (*Collector, error) {
	if pool == nil {
		return nil, errors.New("metrics: growth pool is required")
	}
	if clock == nil {
		return nil, errors.New("metrics: growth clock is required")
	}
	return &Collector{
		Pool:       pool,
		Clock:      clock,
		Disk:       localDisk{},
		MainTables: append([]string(nil), defaultTables...),
		TopN:       10,
	}, nil
}

// Name identifies the collector.
func (c *Collector) Name() string { return "growth" }

// Collect gathers approximate growth samples; missing Redis/Search are skipped.
func (c *Collector) Collect(ctx context.Context) ([]domain.Sample, error) {
	if ctx == nil {
		return nil, errors.New("metrics: growth collect context is required")
	}
	now := c.Clock.Now()
	var samples []domain.Sample
	tableSamples, err := c.tableRows(ctx, now)
	if err != nil {
		return nil, err
	}
	samples = append(samples, tableSamples...)
	var dbBytes int64
	if err := c.Pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&dbBytes); err != nil {
		return nil, fmt.Errorf("metrics: database size: %w", err)
	}
	samples = append(samples, domain.Sample{Name: domain.MetricGrowthDatabaseBytes, At: now, Value: float64(dbBytes)})
	top, err := c.topTables(ctx, now)
	if err != nil {
		return nil, err
	}
	samples = append(samples, top...)
	samples = append(samples, c.optionalRedis(ctx, now)...)
	samples = append(samples, c.optionalSearch(ctx, now)...)
	samples = append(samples, c.mediaSamples(now)...)
	return samples, nil
}

func (c *Collector) tableRows(ctx context.Context, now time.Time) ([]domain.Sample, error) {
	tables := c.MainTables
	if len(tables) == 0 {
		tables = defaultTables
	}
	samples := make([]domain.Sample, 0, len(tables))
	const estimateSQL = `SELECT coalesce(sum(greatest(rel.reltuples, 0)), 0)::bigint
FROM pg_class rel
WHERE rel.oid = $1::regclass
   OR rel.oid IN (SELECT inhrelid FROM pg_inherits WHERE inhparent = $1::regclass)`
	for _, table := range tables {
		var estimate int64
		if err := c.Pool.QueryRow(ctx, estimateSQL, table).Scan(&estimate); err != nil {
			return nil, fmt.Errorf("metrics: estimate %s: %w", table, err)
		}
		samples = append(samples, domain.Sample{
			Name:   domain.MetricGrowthTableRowsApprox,
			Labels: map[string]string{"table": table},
			At:     now,
			Value:  float64(estimate),
		})
	}
	return samples, nil
}

func (c *Collector) topTables(ctx context.Context, now time.Time) ([]domain.Sample, error) {
	limit := c.TopN
	if limit <= 0 {
		limit = 10
	}
	rows, err := c.Pool.Query(ctx, `
SELECT relname, pg_total_relation_size(oid)
FROM pg_class
WHERE relkind = 'r' AND relnamespace = 'public'::regnamespace
ORDER BY pg_total_relation_size(oid) DESC
LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("metrics: top tables: %w", err)
	}
	defer rows.Close()
	var samples []domain.Sample
	for rows.Next() {
		var name string
		var size int64
		if err := rows.Scan(&name, &size); err != nil {
			return nil, fmt.Errorf("metrics: scan top table: %w", err)
		}
		samples = append(samples, domain.Sample{
			Name:   domain.MetricGrowthTableBytes,
			Labels: map[string]string{"table": name},
			At:     now,
			Value:  float64(size),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metrics: iterate top tables: %w", err)
	}
	return samples, nil
}

func (c *Collector) optionalRedis(ctx context.Context, now time.Time) []domain.Sample {
	if c.Redis == nil {
		return nil
	}
	used, err := c.Redis.UsedMemory(ctx)
	if err != nil {
		return nil
	}
	keys, err := c.Redis.KeyCount(ctx)
	if err != nil {
		return nil
	}
	return []domain.Sample{
		{Name: domain.MetricGrowthRedisUsedMemory, At: now, Value: float64(used)},
		{Name: domain.MetricGrowthRedisKeys, At: now, Value: float64(keys)},
	}
}

func (c *Collector) optionalSearch(ctx context.Context, now time.Time) []domain.Sample {
	if c.Search == nil {
		return nil
	}
	docs, err := c.Search.DocumentCount(ctx)
	if err != nil {
		return nil
	}
	store, err := c.Search.StoreBytes(ctx)
	if err != nil {
		return nil
	}
	return []domain.Sample{
		{Name: domain.MetricGrowthESDocs, At: now, Value: float64(docs)},
		{Name: domain.MetricGrowthESStoreBytes, At: now, Value: float64(store)},
	}
}

func (c *Collector) mediaSamples(now time.Time) []domain.Sample {
	path := c.MediaPath
	if path == "" {
		return nil
	}
	disk := c.Disk
	if disk == nil {
		disk = localDisk{}
	}
	used, free, err := disk.Usage(path)
	if err != nil {
		return nil
	}
	return []domain.Sample{
		{Name: domain.MetricGrowthMediaUsedBytes, At: now, Value: float64(used)},
		{Name: domain.MetricGrowthMediaFreeBytes, At: now, Value: float64(free)},
	}
}

type localDisk struct{}

func (localDisk) Usage(path string) (used, free uint64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, fmt.Errorf("metrics: statfs %s: %w", path, err)
	}
	if _, err := os.Stat(path); err != nil {
		return 0, 0, fmt.Errorf("metrics: stat media path: %w", err)
	}
	bsize := stat.Bsize
	if bsize <= 0 {
		return 0, 0, fmt.Errorf("metrics: invalid block size %d", bsize)
	}
	blockSize := uint64(bsize) //nolint:gosec // G115: bsize validated positive
	total := stat.Blocks * blockSize
	free = uint64(stat.Bavail) * blockSize //nolint:gosec // G115: filesystem free blocks
	if total >= free {
		used = total - free
	}
	return used, free, nil
}
