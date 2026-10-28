package roles

import (
	"context"
	"encoding/base64"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/media/app"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/queue"
	quotaRedis "github.com/jooservices/xflickr/internal/quota/adapters/redis"
	scheduleapp "github.com/jooservices/xflickr/internal/schedule/app"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

func TestCrawlAndSchedulerWiring(t *testing.T) {
	pool := &pgxpool.Pool{}
	client := redisv9.NewClient(&redisv9.Options{Addr: "127.0.0.1:1"})
	defer client.Close()
	flickr := flickrclient.NewClient("http://127.0.0.1:1", nil)
	if _, err := newCrawlEngine(pool, client, config.Config{}, flickr); err == nil {
		t.Fatal("crawl engine accepted missing encryption configuration")
	}
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	cfg := config.Config{Crypto: config.CryptoConfig{MasterKeys: "test:" + key, ActiveMasterKeyID: "test"}}
	engine, err := newCrawlEngine(pool, client, cfg, flickr)
	if err != nil || engine == nil || engine.Registry == nil || engine.Store == nil || engine.Keys == nil || engine.Governor == nil || engine.Flickr == nil || engine.Settings == nil {
		t.Fatalf("crawl engine=%+v err=%v", engine, err)
	}
	if _, err := newSchedulerPump(nil, quotaRedis.Governor{}, nil, nil, scheduleapp.PumpSettings{}); err == nil {
		t.Fatal("scheduler accepted a nil database")
	}
	pump, err := newSchedulerPump(pool, quotaRedis.Governor{}, nil, nil, scheduleapp.PumpSettings{})
	if err != nil || pump.PhotoLane == nil || pump.ChangeChecks == nil {
		t.Fatalf("scheduler pump=%+v err=%v", pump, err)
	}
	withPressure, err := newSchedulerPump(pool, quotaRedis.Governor{}, nil, nil, scheduleapp.PumpSettings{}, blobStoreStub{})
	if err != nil || withPressure.MediaPressure == nil {
		t.Fatalf("scheduler pump media pressure=%+v err=%v", withPressure.MediaPressure, err)
	}
}

func TestRegisterPhotoSizesAndMediaRolePreconditions(t *testing.T) {
	pool := &pgxpool.Pool{}
	adapter, err := queue.NewRiver(pool, map[string]int{"sync": 1})
	if err != nil {
		t.Fatal(err)
	}
	client := redisv9.NewClient(&redisv9.Options{Addr: "127.0.0.1:1"})
	defer client.Close()
	if err := registerPhotoSizes(adapter, pool, client, config.Config{}, flickrclient.NewClient("http://127.0.0.1:1", nil)); err != nil {
		t.Fatal(err)
	}
	if err := registerPhotoSizes(adapter, pool, client, config.Config{}, flickrclient.NewClient("http://127.0.0.1:1", nil)); err == nil {
		t.Fatal("duplicate photo sizes job was accepted")
	}
	if err := (mediaRole{}).Run(nil); err != nil {
		t.Fatalf("media role with nil context: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (mediaRole{}).Run(ctx); err != nil {
		t.Fatalf("media role with canceled context: %v", err)
	}
	t.Setenv("DB_HOST", "")
	if err := (mediaRole{}).Run(context.Background()); err == nil {
		t.Fatal("media role accepted missing database configuration")
	}
}

type blobStoreStub struct{}

func (blobStoreStub) Put(context.Context, string, io.Reader) error { return nil }
func (blobStoreStub) Get(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (blobStoreStub) Delete(context.Context, string) error { return nil }
func (blobStoreStub) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", nil
}

func (blobStoreStub) Stat(context.Context, string) (app.BlobInfo, error) { return app.BlobInfo{}, nil }
func (blobStoreStub) Usage(context.Context) (int64, error)               { return 0, nil }
func (blobStoreStub) Full(context.Context) (bool, error)                 { return false, nil }

var _ app.BlobStore = blobStoreStub{}
