package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/metrics/adapters/postgres"
	"github.com/jooservices/xflickr/internal/platform/metrics/app"
	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

const readSeriesSQL = `SELECT bucket, value
FROM metric_samples
WHERE name = $1 AND labels_hash = $2 AND bucket >= $3 AND bucket < $4
ORDER BY bucket ASC
LIMIT 60`

func TestReadSeries(t *testing.T) {
	pool := integrationPool(t)
	store := postgres.Store{Pool: pool}
	seriesStore := postgres.SeriesStore{Pool: pool}
	from := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	to := from.Add(3 * time.Minute)
	queue := fmt.Sprintf("series-test-%d", time.Now().UnixNano())
	node := queue + "-node"
	labels := func(q, role, n string) map[string]string {
		return map[string]string{"queue": q, "role": role, "node": n}
	}
	allLabels := []map[string]string{
		labels(queue, "scheduler", node),
		labels(queue, "scheduler", node),
		labels(queue, "scheduler", node),
		labels(queue, "scheduler", node),
		labels(queue+"-other", "scheduler", node),
		labels(queue, "worker", node),
		labels(queue, "scheduler", node+"-other"),
	}
	cleanupSeriesSamples(t, pool, queue)
	samples := []domain.Sample{
		{Name: domain.MetricQueueJobsAvailable, Labels: allLabels[0], At: from, Value: 7},
		{Name: domain.MetricQueueJobsAvailable, Labels: allLabels[1], At: from.Add(time.Minute), Value: 0},
		{Name: domain.MetricQueueJobsAvailable, Labels: allLabels[2], At: from.Add(2 * time.Minute), Value: 11},
		{Name: domain.MetricQueueJobsAvailable, Labels: allLabels[3], At: to, Value: 99},
		{Name: domain.MetricQueueJobsAvailable, Labels: allLabels[4], At: from, Value: 80},
		{Name: domain.MetricQueueJobsAvailable, Labels: allLabels[5], At: from, Value: 81},
		{Name: domain.MetricQueueJobsAvailable, Labels: allLabels[6], At: from, Value: 82},
	}
	if err := store.WriteBatch(t.Context(), samples); err != nil {
		t.Fatal(err)
	}
	points, err := seriesStore.ReadSeries(t.Context(), app.SeriesQuery{Queue: queue, Role: "scheduler", Node: node, From: from, To: to})
	if err != nil {
		t.Fatal(err)
	}
	want := []app.Point{{At: from, Value: 7}, {At: from.Add(time.Minute), Value: 0}, {At: from.Add(2 * time.Minute), Value: 11}}
	if len(points) != len(want) {
		t.Fatalf("points=%+v, want %+v", points, want)
	}
	for i := range want {
		if points[i] != want[i] {
			t.Errorf("point %d=%+v, want %+v", i, points[i], want[i])
		}
	}
	empty, err := seriesStore.ReadSeries(t.Context(), app.SeriesQuery{Queue: queue + "-empty", Role: "scheduler", Node: node, From: from, To: to})
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty series=%v err=%v", empty, err)
	}
}

func TestReadSeriesPlanUsesIndex(t *testing.T) {
	pool := integrationPool(t)
	store := postgres.Store{Pool: pool}
	from := time.Now().UTC().Truncate(time.Minute).Add(-time.Minute)
	to := from.Add(time.Minute)
	queue := fmt.Sprintf("series-plan-%d", time.Now().UnixNano())
	cleanupSeriesSamples(t, pool, queue)
	const fixtureRows = 5000
	samples := make([]domain.Sample, 0, fixtureRows)
	for i := 0; i < fixtureRows; i++ {
		samples = append(samples, domain.Sample{
			Name: domain.MetricQueueJobsAvailable,
			Labels: map[string]string{
				"queue": queue + "-" + strconv.Itoa(i),
				"role":  "scheduler",
				"node":  "plan-node-" + strconv.Itoa(i),
			},
			At:    from,
			Value: float64(i),
		})
	}
	if err := store.WriteBatch(t.Context(), samples); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "ANALYZE metric_samples"); err != nil {
		t.Fatal(err)
	}
	labelsHash, err := domain.LabelsHash(map[string]string{"queue": queue + "-0", "role": "scheduler", "node": "plan-node-0"})
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := pool.QueryRow(t.Context(), "EXPLAIN (FORMAT JSON) "+readSeriesSQL, domain.MetricQueueJobsAvailable, labelsHash, from, to).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plan []map[string]any
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		t.Fatalf("decode query plan: %v", err)
	}
	if len(plan) != 1 {
		t.Fatalf("query plan entries=%d", len(plan))
	}
	indexSeen := false
	var walk func(map[string]any)
	walk = func(node map[string]any) {
		if nodeType, _ := node["Node Type"].(string); nodeType == "Seq Scan" || nodeType == "Parallel Seq Scan" {
			t.Errorf("query plan contains %s: %s", nodeType, raw)
		}
		if indexName, _ := node["Index Name"].(string); indexName == "metric_samples_unique" {
			indexSeen = true
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			if childNode, ok := child.(map[string]any); ok {
				walk(childNode)
			}
		}
	}
	root, _ := plan[0]["Plan"].(map[string]any)
	if root == nil {
		t.Fatal("query plan has no root Plan")
	}
	walk(root)
	if !indexSeen {
		t.Fatalf("query plan does not use metric_samples_unique: %s", raw)
	}
}

func cleanupSeriesSamples(t *testing.T, pool *pgxpool.Pool, queuePrefix string) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM metric_samples WHERE name = $1 AND labels->>'queue' LIKE $2`, domain.MetricQueueJobsAvailable, queuePrefix+"%"); err != nil {
			t.Errorf("delete metric series fixture: %v", err)
		}
	})
}
