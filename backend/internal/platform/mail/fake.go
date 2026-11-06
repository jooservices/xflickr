package mail

import (
	"context"
	"sync"
)

// Fake records delivered messages for unit tests. It is safe for concurrent
// use and never leaves the process.
type Fake struct {
	mu       sync.Mutex
	messages []Message
	err      error
}

// NewFake creates an empty in-memory sender.
func NewFake() *Fake {
	return &Fake{}
}

// Send validates and records the message unless an injected error is set.
func (f *Fake) Send(ctx context.Context, message Message) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := message.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.messages = append(f.messages, clone(message))
	return nil
}

// Sent returns a copy of every delivered message in send order.
func (f *Fake) Sent() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	messages := make([]Message, len(f.messages))
	for index, message := range f.messages {
		messages[index] = clone(message)
	}
	return messages
}

// SetError makes subsequent sends fail with err until it is cleared with nil.
func (f *Fake) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func clone(message Message) Message {
	message.To = append([]string(nil), message.To...)
	return message
}

var _ Sender = (*Fake)(nil)
