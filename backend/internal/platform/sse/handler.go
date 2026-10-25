package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// UserID resolves the authenticated user for a stream request.
type UserID func(*http.Request) (int64, bool)

// Handler serves an authenticated user-scoped SSE stream.
type Handler struct {
	Hub       *Hub
	Principal UserID
	Heartbeat time.Duration
}

//nolint:cyclop // Authentication, stream setup, and framing share one request lifecycle.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r == nil || r.Context() == nil || h.Hub == nil || h.Principal == nil {
		http.Error(w, "event stream unavailable", http.StatusServiceUnavailable)
		return
	}
	userID, ok := h.Principal(r)
	if !ok || userID <= 0 {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	lastID := r.Header.Get("Last-Event-ID")
	if lastID == "" {
		lastID = r.URL.Query().Get("lastEventId")
	}
	events, cancel, err := h.Hub.Subscribe(userID, lastID)
	if err != nil {
		http.Error(w, "event stream unavailable", http.StatusServiceUnavailable)
		return
	}
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	heartbeat := h.Heartbeat
	if heartbeat <= 0 {
		heartbeat = 25 * time.Second
	}
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-events:
			if !open {
				return
			}
			payload, err := json.Marshal(event.Data)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", safeField(event.ID), safeField(event.Type), payload); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func safeField(value string) string {
	for _, char := range value {
		if char == '\r' || char == '\n' {
			return ""
		}
	}
	return value
}
