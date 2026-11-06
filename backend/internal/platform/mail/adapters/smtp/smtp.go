// Package smtp implements the mail port over SMTP.
package smtp

import (
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	netmail "net/mail"
	netsmtp "net/smtp"
	"strings"
	"time"

	"github.com/jooservices/xflickr/internal/platform/mail"
)

const (
	defaultDialTimeout = 10 * time.Second
	deliveryTimeout    = 30 * time.Second
)

// Sender delivers plain-text messages over SMTP with STARTTLS when the server
// advertises it.
type Sender struct {
	addr     string
	host     string
	from     *netmail.Address
	username string
	password string
}

// New validates the SMTP settings through the port's single implementation.
func New(settings mail.SMTPSettings) (*Sender, error) {
	settings, err := settings.Validate()
	if err != nil {
		return nil, err
	}
	// Validate has already parsed these values, so they cannot fail here.
	host, _, _ := net.SplitHostPort(settings.Addr)
	from, _ := netmail.ParseAddress(settings.From)
	return &Sender{
		addr:     settings.Addr,
		host:     host,
		from:     from,
		username: settings.Username,
		password: settings.Password,
	}, nil
}

// Send validates and delivers one message. Transport failures are returned
// unchanged apart from operation context; nothing is retried here.
func (s *Sender) Send(ctx context.Context, message mail.Message) error {
	if ctx == nil {
		return mail.ErrContextRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := message.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, deliveryTimeout)
	defer cancel()
	if err := s.deliver(ctx, message); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	}
	return nil
}

func (s *Sender) deliver(ctx context.Context, message mail.Message) error {
	dialer := net.Dialer{Timeout: defaultDialTimeout}
	connection, err := dialer.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("mail: dial SMTP %s: %w", s.addr, err)
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.SetDeadline(time.Now()) })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := connection.SetDeadline(deadline); err != nil {
			return fmt.Errorf("mail: set SMTP deadline: %w", err)
		}
	}

	client, err := netsmtp.NewClient(connection, s.host)
	if err != nil {
		return fmt.Errorf("mail: read SMTP greeting: %w", err)
	}
	defer client.Close()
	if err := s.negotiate(client); err != nil {
		return err
	}
	if err := client.Mail(s.from.Address); err != nil {
		return fmt.Errorf("mail: set SMTP sender: %w", err)
	}
	recipients := recipients(message)
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("mail: add SMTP recipient %q: %w", recipient, err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("mail: start SMTP data: %w", err)
	}
	if _, err := writer.Write(format(s.from, recipients, message)); err != nil {
		_ = writer.Close()
		return fmt.Errorf("mail: write SMTP data: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("mail: finish SMTP data: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("mail: close SMTP session: %w", err)
	}
	return nil
}

func (s *Sender) negotiate(client *netsmtp.Client) error {
	if supported, _ := client.Extension("STARTTLS"); supported {
		if err := client.StartTLS(&tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("mail: start SMTP TLS: %w", err)
		}
	}
	if s.username != "" {
		if err := client.Auth(netsmtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
			return fmt.Errorf("mail: authenticate with SMTP: %w", err)
		}
	}
	return nil
}

func recipients(message mail.Message) []string {
	addresses := make([]string, len(message.To))
	for index, recipient := range message.To {
		addresses[index] = strings.TrimSpace(recipient)
	}
	return addresses
}

func format(from *netmail.Address, recipients []string, message mail.Message) []byte {
	var headers strings.Builder
	headers.WriteString("From: ")
	headers.WriteString(from.String())
	headers.WriteString("\r\nTo: ")
	headers.WriteString(strings.Join(recipients, ", "))
	headers.WriteString("\r\nSubject: ")
	headers.WriteString(mime.QEncoding.Encode("utf-8", strings.TrimSpace(message.Subject)))
	headers.WriteString("\r\nDate: ")
	headers.WriteString(time.Now().Format(time.RFC1123Z))
	headers.WriteString("\r\nMIME-Version: 1.0")
	headers.WriteString("\r\nContent-Type: text/plain; charset=UTF-8")
	headers.WriteString("\r\nContent-Transfer-Encoding: 8bit")
	headers.WriteString("\r\n\r\n")
	headers.WriteString(crlf(message.Text))
	if !strings.HasSuffix(headers.String(), "\r\n") {
		headers.WriteString("\r\n")
	}
	return []byte(headers.String())
}

func crlf(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\n", "\r\n")
}

var _ mail.Sender = (*Sender)(nil)
