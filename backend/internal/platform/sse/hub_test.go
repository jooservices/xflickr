package sse

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
)

func TestHubFansOutByUserAndResumes(t *testing.T) {
	hub := NewHub(4)
	first, stopFirst, err := hub.Subscribe(1, "")
	if err != nil {
		t.Fatal(err)
	}
	defer stopFirst()
	other, stopOther, err := hub.Subscribe(2, "")
	if err != nil {
		t.Fatal(err)
	}
	defer stopOther()
	if err := hub.Publish(Event{UserID: 1, Type: "download.progress", Data: []byte(`{"done":1}`)}); err != nil {
		t.Fatal(err)
	}
	event := <-first
	if event.ID == "" || event.Type != "download.progress" {
		t.Fatalf("event = %+v", event)
	}
	select {
	case got := <-other:
		t.Fatalf("user 2 received user 1 event: %+v", got)
	default:
	}
	replay, stopReplay, err := hub.Subscribe(1, "")
	if err != nil {
		t.Fatal(err)
	}
	defer stopReplay()
	if got := <-replay; got.ID != event.ID {
		t.Fatalf("replayed id=%q, want %q", got.ID, event.ID)
	}
	resumed, stopResumed, err := hub.Subscribe(1, event.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer stopResumed()
	select {
	case got := <-resumed:
		t.Fatalf("replayed event at Last-Event-ID: %+v", got)
	default:
	}
}

func TestHubDropsOldestForSlowClientAndValidates(t *testing.T) {
	hub := NewHub(2)
	client, stop, err := hub.Subscribe(3, "")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	for i := 0; i < 20; i++ {
		if err := hub.Publish(Event{UserID: 3, Type: "crawl.progress"}); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(client); got != defaultBuffer {
		t.Fatalf("buffer length=%d, want %d", got, defaultBuffer)
	}
	if err := hub.Publish(Event{UserID: 0, Type: "invalid"}); err == nil {
		t.Fatal("accepted invalid user")
	}
	if err := hub.Publish(Event{UserID: 3, Type: "invalid", Data: []byte("not-json")}); err == nil {
		t.Fatal("accepted invalid payload")
	}
	if _, _, err := hub.Subscribe(0, ""); err == nil {
		t.Fatal("accepted invalid subscriber")
	}
}

func TestHubDefaultsAndPublisherValidation(t *testing.T) {
	hub := NewHub(0)
	if hub.limit != 256 {
		t.Fatalf("default history limit=%d", hub.limit)
	}
	if err := (Publisher{}).Publish(context.Background(), Event{UserID: 1, Type: "test"}); err == nil {
		t.Fatal("publisher accepted missing Redis client")
	}
	if err := (Publisher{}).Listen(context.Background(), hub); err == nil {
		t.Fatal("listener accepted missing Redis client")
	}
	if err := (Publisher{Client: nil}).Publish(nil, Event{UserID: 1, Type: "test"}); err == nil {
		t.Fatal("publisher accepted missing context")
	}
	if (Publisher{}).channel() != "xflickr:events" || (Publisher{Channel: "custom"}).channel() != "custom" {
		t.Fatal("Redis channel defaults are incorrect")
	}
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 10 * time.Millisecond, ReadTimeout: 10 * time.Millisecond, WriteTimeout: 10 * time.Millisecond})
	defer client.Close()
	if err := (Publisher{Client: client}).Publish(context.Background(), Event{UserID: 1, Type: "test"}); err == nil {
		t.Fatal("publisher ignored Redis failure")
	}
	listenCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := (Publisher{Client: client}).Listen(listenCtx, hub); err == nil {
		t.Fatal("listener ignored Redis subscription failure")
	}
	if safeField("line\nbreak") != "" || safeField("safe") != "safe" {
		t.Fatal("SSE field newline validation failed")
	}
}

func TestHandlerStreamsReplayAndRejectsUnauthenticatedUser(t *testing.T) {
	hub := NewHub(4)
	if err := hub.Publish(Event{UserID: 9, ID: "event-1", Type: "download.ready", Data: []byte(`{"status":"ready"}`)}); err != nil {
		t.Fatal(err)
	}
	handler := Handler{Hub: hub, Principal: func(*http.Request) (int64, bool) { return 9, true }, Heartbeat: time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	request.Header.Set("Last-Event-ID", "event-0")
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(response, request)
		close(done)
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not stop after request cancellation")
	}
	body := response.Body.String()
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream" || !strings.Contains(body, "id: event-1\nevent: download.ready\ndata: {\"status\":\"ready\"}") {
		t.Fatalf("status=%d headers=%v body=%q", response.Code, response.Header(), body)
	}
	unauthorized := httptest.NewRecorder()
	(Handler{Hub: hub, Principal: func(*http.Request) (int64, bool) { return 0, false }}).ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/events", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}
}

func TestHandlerRequiresHubAndFlusher(t *testing.T) {
	recorder := httptest.NewRecorder()
	(Handler{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/events", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing dependencies status=%d", recorder.Code)
	}
	response := struct{ http.ResponseWriter }{httptest.NewRecorder()}
	(Handler{Hub: NewHub(1), Principal: func(*http.Request) (int64, bool) { return 1, true }}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/events", nil))
	if response.Header().Get("Content-Type") == "text/event-stream" {
		t.Fatal("stream started without a flusher")
	}
	nilRequest := httptest.NewRecorder()
	(Handler{}).ServeHTTP(nilRequest, nil)
	if nilRequest.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil request status=%d", nilRequest.Code)
	}
}

func TestStreamReturnsResumableEventReader(t *testing.T) {
	hub := NewHub(4)
	if err := hub.Publish(Event{UserID: 44, ID: "stream-1", Type: "crawl.progress", Data: []byte(`{"pending":3}`)}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := (Handler{Hub: hub}).Stream(ctx, 44, "")
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 256)
	n, err := stream.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if !strings.Contains(string(buffer[:n]), "id: stream-1") || !strings.Contains(string(buffer[:n]), "event: crawl.progress") {
		t.Fatalf("stream frame=%q", buffer[:n])
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Read(buffer); err != io.EOF {
		t.Fatalf("closed stream read error=%v", err)
	}
	if _, err := (Handler{}).Stream(context.Background(), 44, ""); err == nil {
		t.Fatal("stream accepted missing hub")
	}
}
