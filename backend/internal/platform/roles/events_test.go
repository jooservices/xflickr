package roles

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	accountshttp "github.com/jooservices/xflickr/internal/accounts/adapters/http"
	crawlhttp "github.com/jooservices/xflickr/internal/crawl/adapters/http"
	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	"github.com/jooservices/xflickr/internal/identity/domain"
	mediahttp "github.com/jooservices/xflickr/internal/media/adapters/http"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/config"
	platformsse "github.com/jooservices/xflickr/internal/platform/sse"
	quotahttp "github.com/jooservices/xflickr/internal/quota/adapters/http"
	schedulehttp "github.com/jooservices/xflickr/internal/schedule/adapters/http"
	sharinghttp "github.com/jooservices/xflickr/internal/sharing/adapters/http"
)

type crawlControlStub struct{ status crawlapp.CrawlStatus }

func (s crawlControlStub) Status(context.Context, int64) (crawlapp.CrawlStatus, error) {
	return s.status, nil
}
func (crawlControlStub) Star(context.Context, int64, string, bool) error { return nil }
func (crawlControlStub) Select(context.Context, int64, string) error     { return nil }
func (crawlControlStub) Pause(context.Context, int64, string) error      { return nil }
func (crawlControlStub) Resume(context.Context, int64, string) error     { return nil }

type eventPublisherStub struct{ event platformsse.Event }

func (s *eventPublisherStub) Publish(_ context.Context, event platformsse.Event) error {
	s.event = event
	return nil
}

func TestAPIEmitsUserScopedCrawlProgress(t *testing.T) {
	publisher := &eventPublisherStub{}
	server := apiServer{
		CrawlHandler: crawlhttp.CrawlHandler{Controls: crawlControlStub{status: crawlapp.CrawlStatus{Depths: []crawlapp.DepthCount{{Depth: 1, Contacts: 2}}}}, ActorID: func(context.Context) (int64, bool) { return 12, true }},
		Events:       publisher,
	}
	ctx := identityhttp.WithPrincipal(context.Background(), identityhttp.Principal{User: domain.User{ID: 12}})
	response, err := server.GetMeCrawl(ctx, gen.GetMeCrawlRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := response.(gen.GetMeCrawl200JSONResponse); !ok {
		t.Fatalf("response type=%T", response)
	}
	if publisher.event.UserID != 12 || publisher.event.Type != "crawl.progress" || len(publisher.event.Data) == 0 {
		t.Fatalf("published event=%+v", publisher.event)
	}
	if id, ok := sseUserID(httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)); !ok || id != 12 {
		t.Fatalf("SSE principal = %d, %v", id, ok)
	}
	if _, ok := sseUserID(httptest.NewRequest(http.MethodGet, "/events", nil)); ok {
		t.Fatal("SSE principal accepted unauthenticated request")
	}
}

func TestSSERoleHelpers(t *testing.T) {
	if _, err := newBlobStore(context.Background(), config.Config{Blob: config.BlobConfig{Driver: "unknown"}}); err == nil {
		t.Fatal("accepted unsupported blob driver")
	}
	blobs, err := newBlobStore(context.Background(), config.Config{Blob: config.BlobConfig{Driver: "local", LocalDir: t.TempDir()}})
	if err != nil || blobs == nil {
		t.Fatalf("local blob store=%v err=%v", blobs, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	listenSSEEvents(ctx, platformsse.Publisher{}, platformsse.NewHub(1))
	if _, err := newBlobStore(context.Background(), config.Config{Blob: config.BlobConfig{Driver: "s3"}}); err == nil {
		t.Fatal("accepted incomplete S3 storage configuration")
	}
}

func TestStreamEventsResponse(t *testing.T) {
	response := httptest.NewRecorder()
	if err := (streamResponse{Body: io.NopCloser(strings.NewReader("id: 1\nevent: download.ready\ndata: {}\n\n"))}).VisitStreamEventsResponse(response); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream" || response.Header().Get("X-Accel-Buffering") != "no" || !strings.Contains(response.Body.String(), "event: download.ready") {
		t.Fatalf("response status=%d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
	if err := (streamResponse{}).VisitStreamEventsResponse(httptest.NewRecorder()); err == nil {
		t.Fatal("nil stream body accepted")
	}
}

func TestAPIStreamEventsUsesLastEventID(t *testing.T) {
	hub := platformsse.NewHub(4)
	if err := hub.Publish(platformsse.Event{UserID: 14, ID: "resume-1", Type: "download.progress", Data: []byte(`{"completed":1}`)}); err != nil {
		t.Fatal(err)
	}
	server := apiServer{Stream: platformsse.Handler{Hub: hub}}
	ctx := identityhttp.WithPrincipal(context.Background(), identityhttp.Principal{User: domain.User{ID: 14}})
	lastID := "resume-0"
	response, err := server.StreamEvents(ctx, gen.StreamEventsRequestObject{Params: gen.StreamEventsParams{LastEventID: &lastID}})
	if err != nil {
		t.Fatal(err)
	}
	stream, ok := response.(streamResponse)
	if !ok {
		t.Fatalf("stream response type=%T", response)
	}
	buffer := make([]byte, 256)
	n, err := stream.Body.Read(buffer)
	if err != nil || !strings.Contains(string(buffer[:n]), "id: resume-1") {
		t.Fatalf("stream read=%q err=%v", buffer[:n], err)
	}
	_ = stream.Body.Close()
	unauthenticated, err := (apiServer{}).StreamEvents(context.Background(), gen.StreamEventsRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := unauthenticated.(gen.StreamEvents401Response); !ok {
		t.Fatalf("unauthenticated response=%T", unauthenticated)
	}
}

func TestAPIRouterProtectsGeneratedSSEEndpoint(t *testing.T) {
	handler := platformsse.Handler{Hub: platformsse.NewHub(2), Principal: sseUserID}
	router := newAPIRouterWithServices(identityhttp.Handler{Version: "test", Sessions: stubSessions{}}, accountshttp.KeyHandler{}, accountshttp.ConnectHandler{}, schedulehttp.Handler{}, crawlhttp.CrawlHandler{}, quotahttp.QuotaHandler{}, sharinghttp.Handler{}, mediahttp.DownloadHandler{}, handler)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/events", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("GET /events status=%d body=%q", response.Code, response.Body.String())
	}
}
