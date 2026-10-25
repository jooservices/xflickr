// Package sse provides user-scoped server-sent event fan-out.
package sse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	redis "github.com/redis/go-redis/v9"
)

const defaultBuffer = 16

// Event is a user-scoped event delivered to SSE clients.
type Event struct {
	ID     string          `json:"id"`
	UserID int64           `json:"user_id"`
	Type   string          `json:"type"`
	Data   json.RawMessage `json:"data"`
}

// Hub fans events out to subscribers on this API instance.
type Hub struct {
	mu      sync.RWMutex
	next    uint64
	clients map[int64]map[chan Event]struct{}
	history []Event
	limit   int
}

func NewHub(historyLimit int) *Hub {
	if historyLimit < 1 {
		historyLimit = 256
	}
	return &Hub{clients: make(map[int64]map[chan Event]struct{}), limit: historyLimit}
}

// Publish assigns an event ID when absent and fans the event out to only the
// target user's active streams. Slow clients drop their oldest queued event.
func (h *Hub) Publish(event Event) error {
	if h == nil || event.UserID <= 0 || event.Type == "" {
		return errors.New("sse: event requires hub, user, and type")
	}
	if len(event.Data) == 0 {
		event.Data = json.RawMessage("null")
	} else if !json.Valid(event.Data) {
		return errors.New("sse: event data must be valid JSON")
	}
	if event.ID == "" {
		event.ID = newEventID(atomic.AddUint64(&h.next, 1))
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.history = append(h.history, event)
	if len(h.history) > h.limit {
		copy(h.history, h.history[len(h.history)-h.limit:])
		h.history = h.history[:h.limit]
	}
	for client := range h.clients[event.UserID] {
		select {
		case client <- event:
		default:
			select {
			case <-client:
			default:
			}
			select {
			case client <- event:
			default:
			}
		}
	}
	return nil
}

// Subscribe registers a client and replays retained events newer than lastID.
func (h *Hub) Subscribe(userID int64, lastID string) (<-chan Event, func(), error) {
	if h == nil || userID <= 0 {
		return nil, nil, errors.New("sse: positive user id and hub are required")
	}
	client := make(chan Event, defaultBuffer)
	h.mu.Lock()
	if h.clients[userID] == nil {
		h.clients[userID] = make(map[chan Event]struct{})
	}
	h.clients[userID][client] = struct{}{}
	for _, event := range h.history {
		if event.UserID == userID && event.ID > lastID {
			select {
			case client <- event:
			default:
				break
			}
		}
	}
	h.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.clients[userID], client)
			if len(h.clients[userID]) == 0 {
				delete(h.clients, userID)
			}
			close(client)
			h.mu.Unlock()
		})
	}
	return client, cancel, nil
}

// Publisher sends typed events to the shared Redis channel for all API nodes.
type Publisher struct {
	Client  redis.UniversalClient
	Channel string
}

func (p Publisher) Publish(ctx context.Context, event Event) error {
	if p.Client == nil || ctx == nil || event.UserID <= 0 || event.Type == "" {
		return errors.New("sse: Redis publisher, context, user, and type are required")
	}
	if event.ID == "" {
		event.ID = newEventID(atomic.AddUint64(&publisherSequence, 1))
	}
	if len(event.Data) == 0 {
		event.Data = json.RawMessage("null")
	}
	if !json.Valid(event.Data) {
		return errors.New("sse: event data must be valid JSON")
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("sse: encode event: %w", err)
	}
	if err := p.Client.Publish(ctx, p.channel(), encoded).Err(); err != nil {
		return fmt.Errorf("sse: publish Redis event: %w", err)
	}
	return nil
}

func (p Publisher) channel() string {
	if p.Channel != "" {
		return p.Channel
	}
	return "xflickr:events"
}

var publisherSequence uint64

func newEventID(sequence uint64) string {
	return fmt.Sprintf("%020d-%020d", time.Now().UnixNano(), sequence)
}

// Listen forwards Redis pub/sub events into the local hub until canceled.
func (p Publisher) Listen(ctx context.Context, hub *Hub) error {
	if p.Client == nil || ctx == nil || hub == nil {
		return errors.New("sse: Redis listener dependencies are required")
	}
	subscription := p.Client.Subscribe(ctx, p.channel())
	defer subscription.Close()
	if _, err := subscription.Receive(ctx); err != nil {
		return fmt.Errorf("sse: subscribe Redis channel: %w", err)
	}
	messages := subscription.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case message, open := <-messages:
			if !open {
				return nil
			}
			var event Event
			if err := json.Unmarshal([]byte(message.Payload), &event); err != nil {
				continue
			}
			if err := hub.Publish(event); err != nil {
				continue
			}
		}
	}
}
