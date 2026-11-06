package smtp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	netmail "net/mail"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/mail"
)

func TestNewUsesPortSettingsValidation(t *testing.T) {
	sender, err := New(mail.SMTPSettings{Addr: "127.0.0.1", From: "no-reply@example.com"})
	if sender != nil || !errors.Is(err, mail.ErrInvalidConfiguration) {
		t.Fatalf("New() = %v, %v; want ErrInvalidConfiguration", sender, err)
	}
}

func TestSendDeliversMessageWithoutAuthentication(t *testing.T) {
	server := startFakeSMTP(t, fakeSMTPOptions{})
	sender := newTestSender(t, mail.SMTPSettings{Addr: server.address(), From: "XFlickr <no-reply@example.com>"})

	message := mail.Message{
		To:      []string{"first@example.com", "second@example.com"},
		Subject: "Verify your email",
		Text:    "Open the link.\nThanks.",
	}
	if err := sender.Send(context.Background(), message); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	received := server.messages()
	if len(received) != 1 {
		t.Fatalf("SMTP messages = %#v, want one", received)
	}
	if received[0].from != "<no-reply@example.com>" {
		t.Errorf("MAIL FROM = %q", received[0].from)
	}
	if strings.Join(received[0].recipients, ",") != "<first@example.com>,<second@example.com>" {
		t.Errorf("RCPT TO = %#v", received[0].recipients)
	}
	for _, want := range []string{
		"From: \"XFlickr\" <no-reply@example.com>",
		"To: first@example.com, second@example.com",
		"Subject: Verify your email",
		"\r\n\r\nOpen the link.\r\nThanks.\r\n",
	} {
		if !strings.Contains(received[0].data, want) {
			t.Errorf("message data missing %q:\n%s", want, received[0].data)
		}
	}
	if server.authenticated() {
		t.Error("sender authenticated against a server that did not require it")
	}
}

func TestSendAuthenticatesWhenCredentialsAreConfigured(t *testing.T) {
	server := startFakeSMTP(t, fakeSMTPOptions{requireAuth: true})
	sender := newTestSender(t, mail.SMTPSettings{
		Addr:     server.address(),
		From:     "no-reply@example.com",
		Username: "user",
		Password: "secret",
	})
	if err := sender.Send(context.Background(), mail.Message{To: []string{"user@example.com"}, Subject: "Verify", Text: "Hello"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !server.authenticated() {
		t.Fatal("sender did not authenticate")
	}
}

func TestSendRejectsInvalidInput(t *testing.T) {
	server := startFakeSMTP(t, fakeSMTPOptions{})
	sender := newTestSender(t, mail.SMTPSettings{Addr: server.address(), From: "no-reply@example.com"})

	if err := sender.Send(nil, mail.Message{}); !errors.Is(err, mail.ErrContextRequired) {
		t.Fatalf("Send(nil) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sender.Send(ctx, mail.Message{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Send(canceled) error = %v", err)
	}
	if err := sender.Send(context.Background(), mail.Message{}); !errors.Is(err, mail.ErrInvalidMessage) {
		t.Fatalf("Send(invalid) error = %v", err)
	}
}

func TestSendReportsConversationFailures(t *testing.T) {
	tests := []struct {
		name         string
		fail         string
		requireAuth  bool
		useAuth      bool
		wantContains string
	}{
		{name: "greeting", fail: "greeting", wantContains: "greeting"},
		{name: "authentication", fail: "auth", requireAuth: true, useAuth: true, wantContains: "authenticate"},
		{name: "sender", fail: "mail", wantContains: "sender"},
		{name: "recipient", fail: "rcpt", wantContains: "recipient"},
		{name: "data start", fail: "data", wantContains: "data"},
		{name: "data finish", fail: "data-end", wantContains: "data"},
		{name: "quit", fail: "quit", wantContains: "session"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := startFakeSMTP(t, fakeSMTPOptions{fail: test.fail, requireAuth: test.requireAuth})
			config := mail.SMTPSettings{Addr: server.address(), From: "no-reply@example.com"}
			if test.useAuth {
				config.Username = "user"
				config.Password = "secret"
			}
			sender := newTestSender(t, config)
			err := sender.Send(context.Background(), mail.Message{To: []string{"user@example.com"}, Subject: "Verify", Text: "Hello"})
			if err == nil || !strings.Contains(err.Error(), test.wantContains) {
				t.Fatalf("Send() error = %v, want an error containing %q", err, test.wantContains)
			}
		})
	}
}

func TestSendReportsDialFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	sender, err := New(mail.SMTPSettings{Addr: address, From: "no-reply@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(context.Background(), mail.Message{To: []string{"user@example.com"}, Subject: "Verify", Text: "Hello"}); err == nil {
		t.Fatal("Send() succeeded without an SMTP server")
	}
}

func TestSendReturnsContextDeadlineWhenServerDoesNotGreet(t *testing.T) {
	server := startFakeSMTP(t, fakeSMTPOptions{silent: true})
	sender := newTestSender(t, mail.SMTPSettings{Addr: server.address(), From: "no-reply@example.com"})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := sender.Send(ctx, mail.Message{To: []string{"user@example.com"}, Subject: "Verify", Text: "Hello"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Send() error = %v, want context deadline", err)
	}
}

func TestFormatEncodesHeadersAndNormalizesBody(t *testing.T) {
	from := &netmail.Address{Name: "XFlickr", Address: "no-reply@example.com"}
	data := string(format(from, []string{"user@example.com"}, mail.Message{
		Subject: "Xin chào",
		Text:    "line one\nline two",
	}))
	if !strings.Contains(data, "Subject: =?utf-8?") {
		t.Errorf("subject was not encoded:\n%s", data)
	}
	if !strings.Contains(data, "\r\n\r\nline one\r\nline two\r\n") {
		t.Errorf("body was not normalized:\n%s", data)
	}
	if strings.Contains(strings.ReplaceAll(data, "\r\n", ""), "\n") {
		t.Errorf("message contains a bare newline:\n%s", data)
	}
}

func newTestSender(t *testing.T, settings mail.SMTPSettings) *Sender {
	t.Helper()
	sender, err := New(settings)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return sender
}

type fakeSMTPOptions struct {
	fail        string
	requireAuth bool
	silent      bool
}

type receivedMail struct {
	from       string
	recipients []string
	data       string
}

type fakeSMTPServer struct {
	listener    net.Listener
	options     fakeSMTPOptions
	mu          sync.Mutex
	received    []receivedMail
	authSeen    bool
	addressText string
}

func startFakeSMTP(t *testing.T, options fakeSMTPOptions) *fakeSMTPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &fakeSMTPServer{listener: listener, options: options, addressText: listener.Addr().String()}
	go server.accept()
	t.Cleanup(func() { _ = listener.Close() })
	return server
}

func (s *fakeSMTPServer) address() string { return s.addressText }

func (s *fakeSMTPServer) accept() {
	connection, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer connection.Close()
	if s.options.silent {
		_, _ = io.Copy(io.Discard, connection)
		return
	}
	s.serve(connection)
}

func (s *fakeSMTPServer) serve(connection net.Conn) {
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	reply := func(line string) bool {
		if _, err := writer.WriteString(line + "\r\n"); err != nil {
			return false
		}
		return writer.Flush() == nil
	}
	if s.options.fail == "greeting" {
		reply("500 no greeting")
		return
	}
	if !reply("220 fake ESMTP") {
		return
	}
	var from string
	var recipients []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command, argument, _ := strings.Cut(strings.TrimRight(line, "\r\n"), " ")
		switch strings.ToUpper(command) {
		case "EHLO":
			if s.options.requireAuth {
				_, _ = writer.WriteString("250-fake\r\n250-AUTH PLAIN\r\n250 HELP\r\n")
				_ = writer.Flush()
			} else if !reply("250 fake") {
				return
			}
		case "HELO":
			if !reply("250 fake") {
				return
			}
		case "AUTH":
			s.mu.Lock()
			s.authSeen = true
			s.mu.Unlock()
			if s.options.fail == "auth" {
				reply("535 authentication failed")
				return
			}
			if !reply("235 authentication successful") {
				return
			}
		case "MAIL":
			from = strings.TrimPrefix(argument, "FROM:")
			if s.options.fail == "mail" {
				if !reply("550 rejected") {
					return
				}
				continue
			}
			if !reply("250 ok") {
				return
			}
		case "RCPT":
			recipients = append(recipients, strings.TrimPrefix(argument, "TO:"))
			if s.options.fail == "rcpt" {
				if !reply("550 rejected") {
					return
				}
				continue
			}
			if !reply("250 ok") {
				return
			}
		case "DATA":
			if s.options.fail == "data" {
				if !reply("554 rejected") {
					return
				}
				continue
			}
			if !reply("354 end with .") {
				return
			}
			data, ok := readData(reader)
			if !ok {
				return
			}
			if s.options.fail == "data-end" {
				reply("554 rejected")
				continue
			}
			s.mu.Lock()
			s.received = append(s.received, receivedMail{from: from, recipients: append([]string(nil), recipients...), data: data})
			s.mu.Unlock()
			if !reply("250 queued") {
				return
			}
		case "RSET":
			if !reply("250 ok") {
				return
			}
		case "QUIT":
			if s.options.fail == "quit" {
				reply("500 rejected")
				return
			}
			reply("221 bye")
			return
		default:
			if !reply("250 ok") {
				return
			}
		}
	}
}

func readData(reader *bufio.Reader) (string, bool) {
	var data strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", false
		}
		if line == ".\r\n" || line == ".\n" {
			return data.String(), true
		}
		data.WriteString(line)
	}
}

func (s *fakeSMTPServer) messages() []receivedMail {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]receivedMail(nil), s.received...)
}

func (s *fakeSMTPServer) authenticated() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authSeen
}

// deadlineContext models the interval before a context timer publishes its error.
type deadlineContext struct {
	context.Context
	deadline time.Time
}

func (c deadlineContext) Deadline() (time.Time, bool) { return c.deadline, true }

func TestDeliveryError(t *testing.T) {
	past := deadlineContext{context.Background(), time.Now().Add(-time.Hour)}
	future := deadlineContext{context.Background(), time.Now().Add(time.Hour)}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer stop()
	transport := errors.New("SMTP rejected")
	timeout := fmt.Errorf("mail: read SMTP greeting: %w", os.ErrDeadlineExceeded)
	netTimeout := fmt.Errorf("mail: read SMTP greeting: %w", &net.DNSError{IsTimeout: true})
	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want error
	}{
		{"socket deadline before context timer", past, timeout, context.DeadlineExceeded},
		{"network timeout before context timer", past, netTimeout, context.DeadlineExceeded},
		{"cancellation wins", canceled, timeout, context.Canceled},
		{"published context deadline", expired, transport, context.DeadlineExceeded},
		{"future deadline", future, timeout, timeout},
		{"no context deadline", context.Background(), timeout, timeout},
		{"unrelated failure after deadline", past, transport, transport},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := deliveryError(tt.ctx, tt.err)
			if !errors.Is(err, tt.want) {
				t.Fatalf("deliveryError() = %v, want %v", err, tt.want)
			}
			if errors.Is(tt.want, context.DeadlineExceeded) && tt.ctx.Err() == nil && !strings.Contains(err.Error(), "read SMTP greeting") {
				t.Fatalf("operation context lost: %v", err)
			}
		})
	}
}
