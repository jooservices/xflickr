package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/quota/app"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

func TestCallLogPartitionsBatchAndRollupIntegration(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, cfg)

	var userID, keyID int64
	email := fmt.Sprintf("call-log-%d@example.invalid", time.Now().UnixNano())
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role, locale) VALUES ($1, 'integration-hash', 'user', 'en') RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID); err != nil {
			t.Errorf("cleanup call-log user: %v", err)
		}
	})
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys (user_id, label, key_public, secret_sealed, per_hour_limit, per_second_limit, burst) VALUES ($1, 'call-log-test', $2, '{}'::jsonb, 100, 100, 100) RETURNING id`, userID, fmt.Sprintf("call-log-%d", time.Now().UnixNano())).Scan(&keyID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	manager := Partitions{Pool: pool}
	if err := manager.MaintainCallLogPartitions(ctx, today.AddDate(0, 0, -30), today.AddDate(0, 0, 7)); err != nil {
		t.Fatal(err)
	}
	futurePartition := "api_call_log_" + today.AddDate(0, 0, 7).Format("20060102")
	var futureExists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, futurePartition).Scan(&futureExists); err != nil || !futureExists {
		t.Fatalf("future partition exists=%t err=%v", futureExists, err)
	}

	quotaCalls := CallLog{Pool: pool}
	if err := quotaCalls.Record(ctx, keyID, "person", now.Add(-5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	legacyAt := now.Add(-8 * time.Minute)
	if _, err := pool.Exec(ctx, `INSERT INTO api_call_log (key_id, class, called_at) VALUES ($1, 'legacy', $2)`, keyID, legacyAt); err != nil {
		t.Fatalf("legacy call insert: %v", err)
	}
	var legacyTimestampsSynced bool
	if err := pool.QueryRow(ctx, `SELECT at = called_at FROM api_call_log WHERE key_id=$1 AND class='legacy'`, keyID).Scan(&legacyTimestampsSynced); err != nil || !legacyTimestampsSynced {
		t.Fatalf("legacy call timestamps synced=%t err=%v", legacyTimestampsSynced, err)
	}
	bucket := now.Truncate(time.Hour).Add(-time.Hour)
	records := []app.CallRecord{
		{At: bucket.Add(5 * time.Minute), KeyID: keyID, Method: "flickr.people.getInfo", Status: "ok", LatencyMS: 10, Bytes: 100, Lane: "person"},
		{At: bucket.Add(15 * time.Minute), KeyID: keyID, Method: "flickr.people.getInfo", Status: "ok", LatencyMS: 20, Bytes: 200, Lane: "person"},
		{At: bucket.Add(45 * time.Minute), KeyID: keyID, Method: "flickr.people.getInfo", Status: "fail", ErrorCode: int64Pointer(105), LatencyMS: 30, Bytes: 300, Lane: "person"},
	}
	if err := quotaCalls.InsertCalls(ctx, records); err != nil {
		t.Fatal(err)
	}
	var calls, errors, p95 int64
	if err := manager.RollupCalls(ctx, bucket, bucket.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT call_count, errors, p95_ms FROM api_call_rollup WHERE day=$1 AND hour=$2 AND key_id=$3 AND method=$4`, bucket.Format("2006-01-02"), int16(bucket.Hour()), keyID, "flickr.people.getInfo").Scan(&calls, &errors, &p95); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || errors != 1 || p95 != 29 {
		t.Fatalf("rollup calls=%d errors=%d p95=%d; want 3, 1, 29", calls, errors, p95)
	}
	usage, err := quotaCalls.Usage(ctx, keyID, now.Add(-time.Hour))
	if err != nil || usage.ByClass["person"] != 1 {
		t.Fatalf("quota usage counted diagnostic calls: %+v err=%v", usage, err)
	}

	statsAt := time.Now().UTC()
	statsHour := statsAt.Truncate(time.Hour)
	beforeStats, err := quotaCalls.Stats(ctx, "1h")
	if err != nil {
		t.Fatal(err)
	}
	unique := fmt.Sprintf("%d", time.Now().UnixNano())
	method, facet := "flickr.test.observability."+unique, "observability_"+unique
	flickrError := int64(987654)
	okStatus, failStatus := 200, 502
	observerUserID := userID
	observations := []app.CallRecord{
		{
			At: statsAt, StartedAt: statsAt, KeyID: keyID, UserID: &observerUserID, Node: "integration-node", NodeID: "integration-node",
			Method: method, Status: "success", Outcome: "success", LatencyMS: 10, Bytes: 11, ResponseBytes: 11, HTTPStatus: &okStatus,
			FlickrStat: "ok", FlickrAccount: "123@example", Lane: "person", Facet: facet, Attempt: 1, RequestID: flickrclient.NewRequestID(),
		},
		{
			At: statsAt, StartedAt: statsAt, KeyID: keyID, UserID: &observerUserID, Node: "integration-node", NodeID: "integration-node",
			Method: method, Status: "failure", Outcome: "failure", ErrorCode: &flickrError, FlickrErrorCode: &flickrError,
			ErrorMessage: "synthetic upstream failure", LatencyMS: 30, Bytes: 13, ResponseBytes: 13, HTTPStatus: &failStatus,
			FlickrStat: "fail", FlickrAccount: "123@example", Lane: "person", Facet: facet, Attempt: 2, RequestID: flickrclient.NewRequestID(),
		},
		{
			At: statsAt, StartedAt: statsAt, KeyID: keyID, UserID: &observerUserID, Node: "integration-node", NodeID: "integration-node",
			Method: method, Status: "failure", Outcome: "failure", ErrorCode: &flickrError, FlickrErrorCode: &flickrError,
			ErrorMessage: "synthetic upstream failure", LatencyMS: 40, Bytes: 13, ResponseBytes: 13, HTTPStatus: &failStatus,
			FlickrStat: "fail", FlickrAccount: "123@example", Lane: "person", Facet: facet, Attempt: 3, RequestID: flickrclient.NewRequestID(),
		},
		{
			At: statsAt, StartedAt: statsAt, KeyID: keyID, UserID: &observerUserID, Node: "integration-node", NodeID: "integration-node",
			Method: method, Status: "failure", Outcome: "failure", ErrorCode: &flickrError, FlickrErrorCode: &flickrError,
			ErrorMessage: "synthetic upstream failure", LatencyMS: 50, Bytes: 13, ResponseBytes: 13, HTTPStatus: &failStatus,
			FlickrStat: "fail", FlickrAccount: "123@example", Lane: "person", Facet: facet, Attempt: 4, RequestID: flickrclient.NewRequestID(),
		},
		{
			At: statsAt, StartedAt: statsAt, KeyID: keyID, UserID: &observerUserID, Node: "integration-node", NodeID: "integration-node",
			Method: method, Status: "quota_wait", Outcome: "quota_wait", Lane: "person", Facet: facet, Attempt: 5, RequestID: flickrclient.NewRequestID(),
		},
	}
	if err := quotaCalls.InsertCalls(ctx, observations); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO work_items (kind, subject_key, user_id, state, updated_at) VALUES ('photos',$1,$2,'done',$4),('person',$3,$2,'done',$4),('favorites',$5,$2,'done',$4)`, "call-stats-photo-"+unique, userID, "call-stats-person-"+unique, statsAt, "call-stats-favorites-"+unique); err != nil {
		t.Fatal(err)
	}
	afterStats, err := quotaCalls.Stats(ctx, "1h")
	if err != nil {
		t.Fatal(err)
	}
	for outcome, want := range map[string]int64{"success": 1, "failure": 3, "quota_wait": 1} {
		if delta := statsCallsAt(beforeStats, statsHour, outcome, afterStats); delta != want {
			t.Fatalf("%s calls in %s increased by %d, want %d", outcome, statsHour, delta, want)
		}
	}
	if !containsCallError(afterStats.TopErrorCodes, flickrError, 3) {
		t.Fatalf("top error codes do not include synthetic error: %+v", afterStats.TopErrorCodes)
	}
	if !containsMethodLatency(afterStats.LatencyByMethod, method, 35, 48.5) {
		t.Fatalf("latency stats do not exclude quota waits or aggregate attempts: %+v", afterStats.LatencyByMethod)
	}
	if !containsFacetRetries(afterStats.RetriesByFacet, facet, 3) {
		t.Fatalf("retry stats do not exclude quota waits: %+v", afterStats.RetriesByFacet)
	}
	beforePhotos, beforePeople := statsCompletedAt(beforeStats, statsHour)
	afterPhotos, afterPeople := statsCompletedAt(afterStats, statsHour)
	if afterPhotos-beforePhotos != 1 || afterPeople-beforePeople != 1 {
		t.Fatalf("completion deltas photos=%d people=%d, want 1/1", afterPhotos-beforePhotos, afterPeople-beforePeople)
	}
	if err := manager.RollupCalls(ctx, statsHour, statsHour.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var observedCalls, observedErrors, observedP95 int64
	if err := pool.QueryRow(ctx, `SELECT call_count, errors, p95_ms FROM api_call_rollup WHERE day=$1 AND hour=$2 AND key_id=$3 AND method=$4`, statsHour.Format("2006-01-02"), int16(statsHour.Hour()), keyID, method).Scan(&observedCalls, &observedErrors, &observedP95); err != nil {
		t.Fatal(err)
	}
	if observedCalls != 4 || observedErrors != 3 || observedP95 != 48 {
		t.Fatalf("rollup included a quota wait or missed failures: calls=%d errors=%d p95=%d, want 4/3/48", observedCalls, observedErrors, observedP95)
	}
	var synced bool
	if err := pool.QueryRow(ctx, `SELECT at = called_at FROM api_call_log WHERE key_id=$1 AND class IS NULL ORDER BY at LIMIT 1`, keyID).Scan(&synced); err != nil || !synced {
		t.Fatalf("call timestamp compatibility at=called_at:%t err=%v", synced, err)
	}

	oldDay := today.AddDate(0, 0, -31)
	if err := manager.MaintainCallLogPartitions(ctx, oldDay, today); err != nil {
		t.Fatal(err)
	}
	if err := quotaCalls.InsertCalls(ctx, []app.CallRecord{{At: oldDay.Add(12 * time.Hour), KeyID: keyID, Method: "flickr.old", Status: "ok"}}); err != nil {
		t.Fatal(err)
	}
	if err := manager.MaintainCallLogPartitions(ctx, today.AddDate(0, 0, -30), today.AddDate(0, 0, 7)); err != nil {
		t.Fatal(err)
	}
	var oldPartitionExists bool
	oldPartition := "api_call_log_" + oldDay.Format("20060102")
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, oldPartition).Scan(&oldPartitionExists); err != nil || oldPartitionExists {
		t.Fatalf("expired partition exists=%t err=%v", oldPartitionExists, err)
	}

	oldRollupDay := today.AddDate(0, 0, -401)
	if _, err := pool.Exec(ctx, `INSERT INTO api_call_rollup (day, hour, key_id, method, call_count, errors, p95_ms) VALUES ($1, 0, $2, 'flickr.old', 1, 0, 1)`, oldRollupDay, keyID); err != nil {
		t.Fatal(err)
	}
	if err := manager.PruneCallRollups(ctx, today.AddDate(0, 0, -400)); err != nil {
		t.Fatal(err)
	}
	var oldRollups int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM api_call_rollup WHERE day=$1 AND key_id=$2`, oldRollupDay, keyID).Scan(&oldRollups); err != nil || oldRollups != 0 {
		t.Fatalf("expired rollups=%d err=%v", oldRollups, err)
	}
}

func int64Pointer(value int64) *int64 { return &value }

func statsCallsAt(before app.CallStats, hour time.Time, outcome string, after app.CallStats) int64 {
	count := func(stats app.CallStats) int64 {
		var total int64
		for _, row := range stats.CallsPerHour {
			if row.Hour.Equal(hour) && row.Outcome == outcome {
				total += row.Calls
			}
		}
		return total
	}
	return count(after) - count(before)
}

func containsCallError(rows []app.CallErrorCode, code, calls int64) bool {
	for _, row := range rows {
		if row.Code == code && row.Calls >= calls {
			return true
		}
	}
	return false
}

func containsMethodLatency(rows []app.CallMethodLatency, method string, p50, p95 float64) bool {
	for _, row := range rows {
		if row.Method == method && row.P50MS == p50 && row.P95MS == p95 {
			return true
		}
	}
	return false
}

func containsFacetRetries(rows []app.CallFacetRetries, facet string, retries int64) bool {
	for _, row := range rows {
		if row.Facet == facet && row.Retries == retries {
			return true
		}
	}
	return false
}

func statsCompletedAt(stats app.CallStats, hour time.Time) (int64, int64) {
	for _, row := range stats.CompletedPerHour {
		if row.Hour.Equal(hour) {
			return row.Photos, row.People
		}
	}
	return 0, 0
}
