package roles

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"

	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	platformsse "github.com/jooservices/xflickr/internal/platform/sse"
)

func TestRoleBuilderSuccessAndErrorBranches(t *testing.T) {
	pool := &pgxpool.Pool{}
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	cfg := config.Config{Crypto: config.CryptoConfig{MasterKeys: "test:" + key, ActiveMasterKeyID: "test"}}
	client := redisv9.NewClient(&redisv9.Options{Addr: "127.0.0.1:1"})
	defer client.Close()
	if handler, err := buildKeyHandler(pool, cfg); err != nil || handler.Principal == nil {
		t.Fatalf("key handler=%+v err=%v", handler, err)
	}
	if handler, err := buildConnectHandler(pool, client, cfg); err != nil || handler.Principal == nil {
		t.Fatalf("connect handler=%+v err=%v", handler, err)
	}
	if _, err := buildCrawlHandlerWithEvents(pool, nil); err != nil {
		t.Fatalf("crawl handler without event publisher: %v", err)
	}
	if _, err := buildSharingHandler(pool); err != nil {
		t.Fatalf("sharing handler: %v", err)
	}
	if _, err := buildDownloadHandler(pool); err != nil {
		t.Fatalf("download handler without blob store: %v", err)
	}
	if _, err := buildDownloadHandler(pool, blobStoreStub{}); err != nil {
		t.Fatalf("download handler with blob store: %v", err)
	}
	if _, err := newCrawlEngine(nil, client, cfg, nil); err == nil {
		t.Fatal("crawl engine accepted a nil database")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	listenSSEEvents(ctx, platformsse.Publisher{}, platformsse.NewHub(1))
	if _, ok := sseUserID(nil); ok {
		t.Fatal("nil request produced a user")
	}
	if _, ok := sseUserID(httptest.NewRequest("GET", "/", nil)); ok {
		t.Fatal("anonymous request produced a user")
	}
	request := httptest.NewRequest("GET", "/", nil).WithContext(identityhttp.WithPrincipal(context.Background(), identityhttp.Principal{User: domain.User{ID: 9}}))
	if id, ok := sseUserID(request); !ok || id != 9 {
		t.Fatalf("sse user = %d, %v", id, ok)
	}
}

func TestRequeueFailedWorkReturnsConfigurationErrors(t *testing.T) {
	t.Setenv("DB_HOST", "")
	if _, err := RequeueFailedWork(context.Background(), nil); err == nil {
		t.Fatal("requeue accepted missing database configuration")
	}
}
