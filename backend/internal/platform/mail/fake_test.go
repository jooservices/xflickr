package mail

import (
	"context"
	"errors"
	"testing"
)

func TestFakeRecordsMessagesAndCopiesThem(t *testing.T) {
	sender := NewFake()
	message := Message{To: []string{"user@example.com"}, Subject: "Verify", Text: "Hello"}
	if err := sender.Send(context.Background(), message); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	message.To[0] = "changed@example.com"

	sent := sender.Sent()
	if len(sent) != 1 || sent[0].To[0] != "user@example.com" || sent[0].Subject != "Verify" || sent[0].Text != "Hello" {
		t.Fatalf("Sent() = %#v", sent)
	}
	sent[0].To[0] = "changed@example.com"
	if got := sender.Sent(); got[0].To[0] != "user@example.com" {
		t.Fatalf("Sent() returned mutable storage: %#v", got)
	}
	if len(NewFake().Sent()) != 0 {
		t.Fatal("new fake reported messages")
	}
}

func TestFakeRejectsInvalidInput(t *testing.T) {
	sender := NewFake()
	if err := sender.Send(nil, Message{}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Send(nil) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sender.Send(ctx, Message{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Send(canceled) error = %v", err)
	}
	if err := sender.Send(context.Background(), Message{}); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("Send(invalid) error = %v", err)
	}
	if len(sender.Sent()) != 0 {
		t.Fatal("fake recorded a rejected message")
	}
}

func TestFakeInjectsAndClearsErrors(t *testing.T) {
	sender := NewFake()
	failure := errors.New("delivery failed")
	sender.SetError(failure)
	message := Message{To: []string{"user@example.com"}, Subject: "Verify", Text: "Hello"}
	if err := sender.Send(context.Background(), message); !errors.Is(err, failure) {
		t.Fatalf("Send() error = %v, want injected error", err)
	}
	if len(sender.Sent()) != 0 {
		t.Fatal("fake recorded a failed message")
	}
	sender.SetError(nil)
	if err := sender.Send(context.Background(), message); err != nil {
		t.Fatalf("Send() after clearing error = %v", err)
	}
	if len(sender.Sent()) != 1 {
		t.Fatalf("Sent() = %#v, want one message", sender.Sent())
	}
}
