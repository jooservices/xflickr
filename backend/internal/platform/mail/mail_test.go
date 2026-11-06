package mail

import (
	"errors"
	"strings"
	"testing"
)

func TestMessageValidate(t *testing.T) {
	tests := []struct {
		name    string
		message Message
		want    string
	}{
		{name: "valid", message: Message{To: []string{"user@example.com"}, Subject: "Verify", Text: "Hello"}},
		{name: "valid multiple with padding", message: Message{To: []string{" first@example.com ", "second@example.com"}, Subject: " Reset ", Text: "Body"}},
		{name: "no recipients", message: Message{Subject: "Verify", Text: "Hello"}, want: "recipient"},
		{name: "blank recipient", message: Message{To: []string{" "}, Subject: "Verify", Text: "Hello"}, want: "recipient"},
		{name: "malformed recipient", message: Message{To: []string{"not-an-address"}, Subject: "Verify", Text: "Hello"}, want: "recipient"},
		{name: "display name", message: Message{To: []string{"User <user@example.com>"}, Subject: "Verify", Text: "Hello"}, want: "recipient"},
		{name: "recipient line break", message: Message{To: []string{"user@example.com\nBcc: evil@example.com"}, Subject: "Verify", Text: "Hello"}, want: "recipient"},
		{name: "blank subject", message: Message{To: []string{"user@example.com"}, Subject: " ", Text: "Hello"}, want: "subject"},
		{name: "subject line break", message: Message{To: []string{"user@example.com"}, Subject: "Verify\r\nBcc: evil@example.com", Text: "Hello"}, want: "subject"},
		{name: "blank text", message: Message{To: []string{"user@example.com"}, Subject: "Verify", Text: " \n\t"}, want: "text"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.message.Validate()
			if test.want == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidMessage) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want ErrInvalidMessage containing %q", err, test.want)
			}
		})
	}
}
