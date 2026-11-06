// Package mail defines the port that delivers transactional email.
package mail

import (
	"context"
	"errors"
	"fmt"
	netmail "net/mail"
	"strings"
)

var (
	// ErrContextRequired reports a nil context passed to a sender.
	ErrContextRequired = errors.New("mail: context is required")
	// ErrInvalidMessage reports a message without recipients, subject, or text.
	ErrInvalidMessage = errors.New("mail: invalid message")
	// ErrInvalidConfiguration reports unusable adapter settings.
	ErrInvalidConfiguration = errors.New("mail: invalid configuration")
)

// Message is one plain-text email. The sender address belongs to the adapter
// configuration so every message uses the same verified sender.
type Message struct {
	To      []string
	Subject string
	Text    string
}

// Validate checks the fields needed to hand a message to an SMTP server. It
// requires bare recipient addresses without display names and rejects header
// line breaks.
func (m Message) Validate() error {
	if len(m.To) == 0 {
		return fmt.Errorf("%w: at least one recipient is required", ErrInvalidMessage)
	}
	for _, recipient := range m.To {
		address := strings.TrimSpace(recipient)
		parsed, err := netmail.ParseAddress(address)
		if err != nil || parsed.Address != address || strings.ContainsAny(address, "\r\n") {
			return fmt.Errorf("%w: recipient %q is not a bare email address", ErrInvalidMessage, recipient)
		}
	}
	subject := strings.TrimSpace(m.Subject)
	if subject == "" || strings.ContainsAny(subject, "\r\n") {
		return fmt.Errorf("%w: subject is required and must not contain line breaks", ErrInvalidMessage)
	}
	if strings.TrimSpace(m.Text) == "" {
		return fmt.Errorf("%w: text is required", ErrInvalidMessage)
	}
	return nil
}

// Sender delivers one email. Implementations must respect context
// cancellation and return an error without retrying when delivery fails.
type Sender interface {
	Send(context.Context, Message) error
}
