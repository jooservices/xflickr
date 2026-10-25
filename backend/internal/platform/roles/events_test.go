package roles

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	accountshttp "github.com/jooservices/xflickr/internal/accounts/adapters/http"
	crawlhttp "github.com/jooservices/xflickr/internal/crawl/adapters/http"
	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	identityapp "github.com/jooservices/xflickr/internal/identity/app"
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

func TestCrawlProgressNotifierReachesStreamWithoutPolling(t *testing.T) {
	hub := platformsse.NewHub(8)
	stream, cancel, err := hub.Subscribe(12, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	other, cancelOther, err := hub.Subscribe(13, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cancelOther()
	notifier := crawlProgressNotifier{
		Status: crawlControlStub{status: crawlapp.CrawlStatus{Depths: []crawlapp.DepthCount{{Depth: 1, Contacts: 2}}}},
		Events: hubPublisher{hub},
	}
	engine := &crawlapp.Engine{Progress: notifier}
	if err := engine.Progress.CrawlChanged(context.Background(), 12); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-stream:
		if event.UserID != 12 || event.Type != "crawl.progress" || !strings.Contains(string(event.Data), `"contacts":2`) {
			t.Fatalf("event=%+v", event)
		}
	default:
		t.Fatal("crawl state change did not reach the stream")
	}
	select {
	case event := <-other:
		t.Fatalf("event leaked to another user: %+v", event)
	default:
	}
	if err := (crawlProgressNotifier{}).CrawlChanged(context.Background(), 12); err == nil {
		t.Fatal("notifier accepted missing dependencies")
	}
	if err := (crawlProgressNotifier{Status: failingStatus{}, Events: hubPublisher{hub}}).CrawlChanged(context.Background(), 12); err == nil {
		t.Fatal("notifier ignored status failure")
	}
	if id, ok := sseUserID(httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(identityhttp.WithPrincipal(context.Background(), identityhttp.Principal{User: domain.User{ID: 12}}))); !ok || id != 12 {
		t.Fatalf("SSE principal = %d, %v", id, ok)
	}
	if _, ok := sseUserID(httptest.NewRequest(http.MethodGet, "/events", nil)); ok {
		t.Fatal("SSE principal accepted unauthenticated request")
	}
}

type hubPublisher struct{ hub *platformsse.Hub }

func (p hubPublisher) Publish(_ context.Context, event platformsse.Event) error {
	return p.hub.Publish(event)
}

type failingStatus struct{}

func (failingStatus) Status(context.Context, int64) (crawlapp.CrawlStatus, error) {
	return crawlapp.CrawlStatus{}, errors.New("status unavailable")
}

func TestBuildAPIHandlerRequiresBlobDriver(t *testing.T) {
	if _, err := newBlobStore(context.Background(), config.Config{}); err == nil {
		t.Fatal("empty blob driver accepted; download back-pressure could be disabled")
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

type cookieSessions struct{ stubSessions }

func (cookieSessions) Authenticate(_ context.Context, token string) (identityapp.Authenticated, error) {
	id, err := strconv.ParseInt(token, 10, 64)
	if err != nil {
		return identityapp.Authenticated{}, identityapp.ErrUnauthenticated
	}
	return identityapp.Authenticated{User: domain.User{ID: id}}, nil
}

func TestEventsRouteResumesWithLastEventIDAndIsolatesUsers(t *testing.T) {
	hub := platformsse.NewHub(64)
	for i := 1; i <= 20; i++ {
		userID := int64(21)
		if i%5 == 0 {
			userID = 22
		}
		if err := hub.Publish(platformsse.Event{UserID: userID, ID: fmt.Sprintf("%03d", i), Type: "download.progress", Data: []byte(fmt.Sprintf(`{"n":%d}`, i))}); err != nil {
			t.Fatal(err)
		}
	}
	router := newAPIRouterWithServices(identityhttp.Handler{Version: "test", Sessions: cookieSessions{}}, accountshttp.KeyHandler{}, accountshttp.ConnectHandler{}, schedulehttp.Handler{}, crawlhttp.CrawlHandler{}, quotahttp.QuotaHandler{}, sharinghttp.Handler{}, mediahttp.DownloadHandler{}, platformsse.Handler{Hub: hub, Principal: sseUserID})
	server := httptest.NewServer(router)
	defer server.Close()
	read := func(user, lastID string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/events", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.AddCookie(&http.Cookie{Name: identityhttp.SessionCookieName, Value: user})
		if lastID != "" {
			request.Header.Set("Last-Event-ID", lastID)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return string(body)
	}
	got := read("21", "")
	for _, id := range []string{"001", "004", "006", "019"} {
		if !strings.Contains(got, "id: "+id+"\n") {
			t.Fatalf("user 21 stream is missing event %s: %q", id, got)
		}
	}
	for _, id := range []string{"005", "010", "015", "020"} {
		if strings.Contains(got, "id: "+id+"\n") {
			t.Fatalf("user 21 received user 22 event %s", id)
		}
	}
	resumed := read("21", "012")
	if strings.Contains(resumed, "id: 011\n") || strings.Contains(resumed, "id: 012\n") || !strings.Contains(resumed, "id: 013\n") || !strings.Contains(resumed, "id: 019\n") {
		t.Fatalf("resume after 012 returned %q", resumed)
	}
}
