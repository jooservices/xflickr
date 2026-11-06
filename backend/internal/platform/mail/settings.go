package mail

import (
	"fmt"
	"net"
	netmail "net/mail"
	"strconv"
	"strings"
)

// SMTPSettings contains the SMTP endpoint and optional authentication. Empty
// credentials select an unauthenticated sink such as Mailpit.
type SMTPSettings struct {
	Addr     string
	From     string
	Username string
	Password string
}

// Validate checks and normalises the SMTP settings used by every adapter. It
// requires a host:port address, a valid sender address, and credentials that
// are either both set or both empty.
func (s SMTPSettings) Validate() (SMTPSettings, error) {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(s.Addr))
	if err != nil || strings.TrimSpace(host) == "" {
		return SMTPSettings{}, fmt.Errorf("%w: SMTP address must be host:port", ErrInvalidConfiguration)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return SMTPSettings{}, fmt.Errorf("%w: SMTP port must be between 1 and 65535", ErrInvalidConfiguration)
	}
	from, err := netmail.ParseAddress(strings.TrimSpace(s.From))
	if err != nil || from.Address == "" {
		return SMTPSettings{}, fmt.Errorf("%w: a valid sender address is required", ErrInvalidConfiguration)
	}
	username := strings.TrimSpace(s.Username)
	if (username == "") != (s.Password == "") {
		return SMTPSettings{}, fmt.Errorf("%w: SMTP username and password must be set together", ErrInvalidConfiguration)
	}
	s.Addr = net.JoinHostPort(host, portText)
	if from.Name == "" {
		s.From = from.Address
	} else {
		s.From = from.String()
	}
	s.Username = username
	return s, nil
}
