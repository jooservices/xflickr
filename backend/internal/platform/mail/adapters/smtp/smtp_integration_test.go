package smtp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	netmail "net/mail"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/mail"
	mailsmtp "github.com/jooservices/xflickr/internal/platform/mail/adapters/smtp"
)

const defaultMailpitAPI = "http://127.0.0.1:18025"

func TestSMTPSendsToMailpitIntegration(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared Mailpit sink")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	address := environment("MAIL_SMTP_ADDR", "127.0.0.1:11025")
	from := environment("MAIL_FROM", "no-reply@xflickr.local")
	fromAddress, err := netmail.ParseAddress(from)
	if err != nil {
		t.Fatalf("parse MAIL_FROM: %v", err)
	}
	sender, err := mailsmtp.New(mailsmtp.Config{Addr: address, From: from})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	token := fmt.Sprintf("mail-port-%d", time.Now().UnixNano())
	recipient := "mail-port+" + token + "@example.invalid"
	subject := "XFlickr mail port " + token
	text := "Integration body " + token
	message := mail.Message{To: []string{recipient}, Subject: subject, Text: text}
	if err := sender.Send(ctx, message); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	id := waitForMessage(t, ctx, subject)
	detail := readMessage(t, ctx, id)
	if detail.Subject != subject {
		t.Errorf("subject = %q, want %q", detail.Subject, subject)
	}
	if detail.From.Address != fromAddress.Address {
		t.Errorf("from = %q, want %q", detail.From.Address, fromAddress.Address)
	}
	if len(detail.To) != 1 || detail.To[0].Address != recipient {
		t.Errorf("to = %#v, want %q", detail.To, recipient)
	}
	if !strings.Contains(detail.Text, token) {
		t.Errorf("message text = %q, want it to contain %q", detail.Text, token)
	}
}

func waitForMessage(t *testing.T, ctx context.Context, subject string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var list mailpitMessages
		getJSON(t, ctx, mailpitAPI()+"/api/v1/messages?limit=100", &list)
		for _, message := range list.Messages {
			if message.Subject == subject {
				return message.ID
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("Mailpit did not receive a message with subject %q", subject)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for Mailpit: %v", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func readMessage(t *testing.T, ctx context.Context, id string) mailpitMessage {
	t.Helper()
	var message mailpitMessage
	getJSON(t, ctx, mailpitAPI()+"/api/v1/message/"+url.PathEscape(id), &message)
	return message
}

func getJSON(t *testing.T, ctx context.Context, endpoint string, target any) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatalf("create Mailpit request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("read Mailpit: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Mailpit %s returned HTTP %d", endpoint, response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode Mailpit response: %v", err)
	}
}

func mailpitAPI() string {
	return strings.TrimRight(environment("MAILPIT_API_URL", defaultMailpitAPI), "/")
}

func environment(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

type mailpitMessages struct {
	Messages []struct {
		ID      string `json:"ID"`
		Subject string `json:"Subject"`
	} `json:"messages"`
}

type mailpitMessage struct {
	From struct {
		Address string `json:"Address"`
	} `json:"From"`
	To []struct {
		Address string `json:"Address"`
	} `json:"To"`
	Subject string `json:"Subject"`
	Text    string `json:"Text"`
}
