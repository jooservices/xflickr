package growth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/platform/metrics/adapters/growth"
	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

func TestGrowthNameAndSuccessfulOptionalSources(t *testing.T) {
	now := time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)
	collector, err := growth.NewCollector(fakePool{
		estimates: map[string]int64{
			"people": 1, "photos": 2, "albums": 3, "galleries": 4, "groups": 5,
			"favorites": 6, "observations": 7, "api_call_log": 8, "work_items": 9, "outbox_events": 10,
		},
		dbSize: 99,
		top: []struct {
			name string
			size int64
		}{{"photos", 50}},
	}, fixedClock{at: now})
	if err != nil {
		t.Fatal(err)
	}
	if collector.Name() != "growth" {
		t.Fatalf("name=%q", collector.Name())
	}
	collector.Redis = fakeRedis{used: 11, keys: 22}
	collector.Search = fakeSearch{docs: 3, store: 4}
	dir := t.TempDir()
	collector.MediaPath = dir
	collector.Disk = nil
	samples, err := collector.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.ValidateSamples(samples); err != nil {
		t.Fatal(err)
	}
	var sawRedis, sawSearch, sawMedia bool
	for _, sample := range samples {
		switch sample.Name {
		case domain.MetricGrowthRedisKeys:
			sawRedis = sample.Value == 22
		case domain.MetricGrowthESDocs:
			sawSearch = sample.Value == 3
		case domain.MetricGrowthMediaFreeBytes:
			sawMedia = sample.Value > 0
		}
	}
	if !sawRedis || !sawSearch || !sawMedia {
		t.Fatalf("redis=%v search=%v media=%v", sawRedis, sawSearch, sawMedia)
	}
	if _, err := growth.NewCollector(nil, fixedClock{at: now}); err == nil {
		t.Fatal("expected nil pool error")
	}
	if _, err := growth.NewCollector(fakePool{}, nil); err == nil {
		t.Fatal("expected nil clock error")
	}
	if _, err := collector.Collect(nil); err == nil {
		t.Fatal("expected nil context error")
	}
}

type fakeSearch struct {
	docs, store int64
}

func (f fakeSearch) DocumentCount(context.Context) (int64, error) { return f.docs, nil }
func (f fakeSearch) StoreBytes(context.Context) (int64, error)    { return f.store, nil }

func TestSearchReaderFetchesStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xflickr_*/_stats/docs,store" && r.URL.Path != "/_all/_stats/docs,store" {
			// prefix path includes star
		}
		_, _ = w.Write([]byte(`{"_all":{"primaries":{"docs":{"count":9},"store":{"size_in_bytes":100}}}}`))
	}))
	t.Cleanup(server.Close)
	reader := &growth.SearchReader{URL: server.URL, Username: "u", Password: "p", IndexPrefix: "xflickr_", Client: server.Client()}
	docs, err := reader.DocumentCount(t.Context())
	if err != nil || docs != 9 {
		t.Fatalf("docs=%d err=%v", docs, err)
	}
	store, err := reader.StoreBytes(t.Context())
	if err != nil || store != 100 {
		t.Fatalf("store=%d err=%v", store, err)
	}
	empty := &growth.SearchReader{}
	if _, err := empty.DocumentCount(t.Context()); err == nil {
		t.Fatal("expected missing URL error")
	}
	badStatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(badStatus.Close)
	bad := &growth.SearchReader{URL: badStatus.URL, Client: badStatus.Client()}
	if _, err := bad.DocumentCount(t.Context()); err == nil {
		t.Fatal("expected status error")
	}
	invalidJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{"))
	}))
	t.Cleanup(invalidJSON.Close)
	broken := &growth.SearchReader{URL: invalidJSON.URL, Client: invalidJSON.Client()}
	if _, err := broken.StoreBytes(t.Context()); err == nil {
		t.Fatal("expected decode error")
	}
	all := &growth.SearchReader{URL: server.URL, Client: server.Client()}
	if _, err := all.DocumentCount(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestParseUsedMemory(t *testing.T) {
	used, err := growth.ParseUsedMemory("# Memory\nused_memory:1234\n")
	if err != nil || used != 1234 {
		t.Fatalf("used=%d err=%v", used, err)
	}
	if _, err := growth.ParseUsedMemory("nope\n"); err == nil {
		t.Fatal("expected missing field")
	}
	if _, err := growth.ParseUsedMemory("used_memory:x\n"); err == nil {
		t.Fatal("expected parse error")
	}
	if _, err := (growth.RedisReader{}).UsedMemory(t.Context()); err == nil {
		t.Fatal("expected nil client")
	}
	if _, err := (growth.RedisReader{}).KeyCount(t.Context()); err == nil {
		t.Fatal("expected nil client")
	}
	reader := growth.RedisReader{Client: fakeMemoryClient{info: "# Memory\nused_memory:9\n", size: 4}}
	used, err = reader.UsedMemory(t.Context())
	if err != nil || used != 9 {
		t.Fatalf("used=%d err=%v", used, err)
	}
	keys, err := reader.KeyCount(t.Context())
	if err != nil || keys != 4 {
		t.Fatalf("keys=%d err=%v", keys, err)
	}
	failing := growth.RedisReader{Client: fakeMemoryClient{err: errBoom}}
	if _, err := failing.UsedMemory(t.Context()); err == nil {
		t.Fatal("expected info error")
	}
	if _, err := failing.KeyCount(t.Context()); err == nil {
		t.Fatal("expected size error")
	}
	if _, err := (growth.GoRedisMemory{}).MemoryInfo(t.Context()); err == nil {
		t.Fatal("expected nil go-redis client")
	}
	if _, err := (growth.GoRedisMemory{}).DBSize(t.Context()); err == nil {
		t.Fatal("expected nil go-redis client")
	}
}

func TestGoRedisMemoryWithMiniredis(t *testing.T) {
	server := miniredis.RunT(t)
	client := redisv9.NewClient(&redisv9.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	adapter := growth.GoRedisMemory{Client: client}
	info, err := adapter.MemoryInfo(t.Context())
	if err != nil || info == "" {
		t.Fatalf("info=%q err=%v", info, err)
	}
	if _, err := adapter.DBSize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := (growth.RedisReader{Client: adapter}).KeyCount(t.Context()); err != nil {
		t.Fatal(err)
	}
}

type fakeMemoryClient struct {
	info string
	size int64
	err  error
}

func (f fakeMemoryClient) MemoryInfo(context.Context) (string, error) { return f.info, f.err }
func (f fakeMemoryClient) DBSize(context.Context) (int64, error)      { return f.size, f.err }

var errBoom = errString("boom")

type errString string

func (e errString) Error() string { return string(e) }

func TestLocalDiskUsage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "blob"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	collector, err := growth.NewCollector(fakePool{
		estimates: map[string]int64{
			"people": 0, "photos": 0, "albums": 0, "galleries": 0, "groups": 0,
			"favorites": 0, "observations": 0, "api_call_log": 0, "work_items": 0, "outbox_events": 0,
		},
		dbSize: 1,
	}, fixedClock{at: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	collector.MainTables = []string{"people"}
	collector.MediaPath = dir
	samples, err := collector.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, sample := range samples {
		if sample.Name == domain.MetricGrowthMediaUsedBytes {
			found = true
		}
	}
	if !found {
		t.Fatal("missing media used sample")
	}
}
