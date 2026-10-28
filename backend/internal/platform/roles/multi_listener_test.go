package roles

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestAPIRoleRunServersServesAndStopsMultipleListeners(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := make(chan net.Listener, 2)
	role := APIRole{listenerFactory: func(string) (net.Listener, error) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			ready <- listener
		}
		return listener, err
	}}
	handlers := []http.Handler{
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "public") }),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "internal") }),
	}
	done := make(chan error, 1)
	go func() {
		done <- role.runServers(ctx, []apiServerSpec{
			{address: "public", handler: handlers[0]},
			{address: "internal", handler: handlers[1]},
		})
	}()
	listeners := make([]net.Listener, 0, 2)
	for range 2 {
		select {
		case listener := <-ready:
			listeners = append(listeners, listener)
		case err := <-done:
			t.Fatalf("listeners failed to start: %v", err)
		case <-time.After(2 * time.Second):
			t.Fatal("listeners did not start")
		}
	}
	for index, listener := range listeners {
		response, err := http.Get(fmt.Sprintf("http://%s/", listener.Addr()))
		if err != nil {
			t.Fatalf("request listener %d: %v", index, err)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil || string(body) != []string{"public", "internal"}[index] {
			t.Fatalf("listener %d body = %q, read error = %v", index, body, readErr)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("listener shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listeners did not stop after cancellation")
	}
}

func TestAPIRoleRunServersRejectsEmptyAndInvalidListeners(t *testing.T) {
	role := APIRole{}
	if err := role.runServers(t.Context(), nil); err == nil {
		t.Fatal("empty listener set was accepted")
	}
	var opened net.Listener
	role.listenerFactory = func(string) (net.Listener, error) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		opened = listener
		return listener, err
	}
	err := role.runServers(t.Context(), []apiServerSpec{
		{address: "first", handler: http.NotFoundHandler()},
		{address: "second"},
	})
	if err == nil {
		t.Fatal("listener without a handler was accepted")
	}
	if opened == nil {
		t.Fatal("first listener was not opened")
	}
	connection, err := net.DialTimeout("tcp", opened.Addr().String(), 100*time.Millisecond)
	if err == nil {
		_ = connection.Close()
		t.Fatal("opened listener leaked after startup failed")
	}
}
