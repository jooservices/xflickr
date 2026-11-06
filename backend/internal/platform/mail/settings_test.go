package mail

import (
	"errors"
	"strings"
	"testing"
)

func TestSMTPSettingsValidate(t *testing.T) {
	tests := []struct {
		name     string
		settings SMTPSettings
		want     SMTPSettings
		wantErr  string
	}{
		{
			name:     "valid without credentials",
			settings: SMTPSettings{Addr: "127.0.0.1:11025", From: "no-reply@example.com"},
			want:     SMTPSettings{Addr: "127.0.0.1:11025", From: "no-reply@example.com"},
		},
		{
			name:     "valid with display name and credentials is normalised",
			settings: SMTPSettings{Addr: " smtp.example.com:587 ", From: " XFlickr <no-reply@example.com> ", Username: " user ", Password: "secret"},
			want:     SMTPSettings{Addr: "smtp.example.com:587", From: "\"XFlickr\" <no-reply@example.com>", Username: "user", Password: "secret"},
		},
		{name: "minimum port", settings: SMTPSettings{Addr: "smtp.example.com:1", From: "no-reply@example.com"}, want: SMTPSettings{Addr: "smtp.example.com:1", From: "no-reply@example.com"}},
		{name: "maximum port and IPv6", settings: SMTPSettings{Addr: "[::1]:65535", From: "no-reply@example.com"}, want: SMTPSettings{Addr: "[::1]:65535", From: "no-reply@example.com"}},
		{name: "non-numeric port", settings: SMTPSettings{Addr: "smtp.example.com:smtp", From: "no-reply@example.com"}, wantErr: "SMTP port"},
		{name: "negative port", settings: SMTPSettings{Addr: "smtp.example.com:-1", From: "no-reply@example.com"}, wantErr: "SMTP port"},
		{name: "missing port", settings: SMTPSettings{Addr: "127.0.0.1", From: "no-reply@example.com"}, wantErr: "SMTP address"},
		{name: "missing host", settings: SMTPSettings{Addr: ":11025", From: "no-reply@example.com"}, wantErr: "SMTP address"},
		{name: "port zero", settings: SMTPSettings{Addr: "127.0.0.1:0", From: "no-reply@example.com"}, wantErr: "SMTP port"},
		{name: "port too high", settings: SMTPSettings{Addr: "127.0.0.1:70000", From: "no-reply@example.com"}, wantErr: "SMTP port"},
		{name: "missing sender", settings: SMTPSettings{Addr: "127.0.0.1:11025"}, wantErr: "sender address"},
		{name: "malformed sender", settings: SMTPSettings{Addr: "127.0.0.1:11025", From: "not an address"}, wantErr: "sender address"},
		{name: "username without password", settings: SMTPSettings{Addr: "127.0.0.1:11025", From: "no-reply@example.com", Username: "user"}, wantErr: "username and password"},
		{name: "password without username", settings: SMTPSettings{Addr: "127.0.0.1:11025", From: "no-reply@example.com", Password: "secret"}, wantErr: "username and password"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.settings.Validate()
			if test.wantErr != "" {
				if !errors.Is(err, ErrInvalidConfiguration) || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("Validate() = %#v, %v; want ErrInvalidConfiguration containing %q", got, err, test.wantErr)
				}
				if got != (SMTPSettings{}) {
					t.Errorf("Validate() on error = %#v, want zero settings", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if got != test.want {
				t.Errorf("Validate() = %#v, want %#v", got, test.want)
			}
		})
	}
}
