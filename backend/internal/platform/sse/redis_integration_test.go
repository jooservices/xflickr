package sse

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	redis "github.com/redis/go-redis/v9"
)

func TestRedisPublisherDeliversProgressToSSEClient(t *testing.T) {
	values, err := godotenv.Read("../../../../.env")
	if err != nil {
		t.Skip("worktree Redis configuration is unavailable")
	}
	host := values["REDIS_HOST"]
	if host == "" {
		host = "127.0.0.1"
	}
	port := 16382
	if raw := values["REDIS_PORT"]; raw != "" {
		port, err = strconv.Atoi(raw)
		if err != nil {
			t.Fatal("invalid worktree Redis port")
		}
	}
	database := 8
	if raw := values["REDIS_DB"]; raw != "" {
		database, err = strconv.Atoi(raw)
		if err != nil {
			t.Fatal("invalid worktree Redis database")
		}
	}
	client := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("%s:%d", host, port), Password: values["REDIS_PASSWORD"], DB: database})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("shared Redis is unavailable: %v", err)
	}
	channel := values["REDIS_PREFIX"] + "events:test:" + fmt.Sprint(time.Now().UnixNano())
	publisher := Publisher{Client: client, Channel: channel}
	hub := NewHub(8)
	listenDone := make(chan error, 1)
	go func() { listenDone <- publisher.Listen(ctx, hub) }()
	secondClient := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("%s:%d", host, port), Password: values["REDIS_PASSWORD"], DB: database})
	t.Cleanup(func() { _ = secondClient.Close() })
	secondHub := NewHub(8)
	secondListenDone := make(chan error, 1)
	secondPublisher := Publisher{Client: secondClient, Channel: channel}
	go func() { secondListenDone <- secondPublisher.Listen(ctx, secondHub) }()
	server := httptest.NewServer(Handler{Hub: hub, Principal: func(*http.Request) (int64, bool) { return 90125, true }, Heartbeat: time.Second})
	t.Cleanup(server.Close)
	secondServer := httptest.NewServer(Handler{Hub: secondHub, Principal: func(*http.Request) (int64, bool) { return 90125, true }, Heartbeat: time.Second})
	t.Cleanup(secondServer.Close)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("SSE response status=%d content-type=%q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	secondRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, secondServer.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	secondResponse, err := http.DefaultClient.Do(secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer secondResponse.Body.Close()
	if err := client.Publish(ctx, channel, "not-json").Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Publish(ctx, channel, `{"user_id":0,"type":"invalid","data":null}`).Err(); err != nil {
		t.Fatal(err)
	}
	subscribeDeadline := time.Now().Add(2 * time.Second)
	for {
		counts, err := client.PubSubNumSub(ctx, channel).Result()
		if err != nil {
			t.Fatal(err)
		}
		if counts[channel] == 2 {
			break
		}
		if time.Now().After(subscribeDeadline) {
			t.Fatalf("Redis subscriber count=%d, want 2", counts[channel])
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := publisher.Publish(ctx, Event{UserID: 90125, Type: "download.progress", Data: []byte(`{"download_id":77,"completed":2,"total":5}`)}); err != nil {
		t.Fatal(err)
	}
	frame := readFrame(t, response)
	secondFrame := readFrame(t, secondResponse)
	for _, frame := range []string{frame, secondFrame} {
		if !strings.Contains(frame, "id: ") || !strings.Contains(frame, "event: download.progress") || !strings.Contains(frame, `"completed":2`) {
			t.Fatalf("unexpected SSE frame: %q", frame)
		}
	}
	cancel()
	select {
	case <-listenDone:
	case <-time.After(time.Second):
		t.Fatal("Redis listener did not stop after cancellation")
	}
	select {
	case <-secondListenDone:
	case <-time.After(time.Second):
		t.Fatal("second Redis listener did not stop after cancellation")
	}
}

func readFrame(t *testing.T, response *http.Response) string {
	t.Helper()
	reader := bufio.NewReader(response.Body)
	var frame strings.Builder
	deadline := time.After(3 * time.Second)
	for !strings.Contains(frame.String(), "\n\n") {
		select {
		case <-deadline:
			t.Fatalf("timed out reading SSE frame: %q", frame.String())
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE frame: %v", err)
		}
		frame.WriteString(line)
	}
	return frame.String()
}
