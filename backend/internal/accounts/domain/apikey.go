// Package domain defines Flickr API keys owned by an XFlickr user.
// The secret is not part of this model. Callers seal it before storage and
// never return it.
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

const (
	// LabelMaxLength is the longest accepted key label, counted in characters.
	LabelMaxLength = 80
	// CredentialMaxLength bounds a Flickr API key or secret before it is checked.
	CredentialMaxLength = 255
	// FlickrHourlyCeiling is Flickr's platform call budget for one key per hour.
	FlickrHourlyCeiling = 3600
	// PublicKeyVisiblePrefix is how many leading characters of the public key
	// are shown. The remainder is masked.
	PublicKeyVisiblePrefix = 6

	// StatusValid means Flickr accepted the key and it may be used.
	StatusValid Status = "valid"
	// StatusInvalid means the key is stored but must not be used until it is
	// accepted again.
	StatusInvalid Status = "invalid"
	// StatusRevoked is a terminal status. A revoked key cannot become valid
	// or invalid again.
	StatusRevoked Status = "revoked"

	// DeletedTopic is the transactional outbox topic published when a key is deleted.
	// Consumers stop queued work for that key.
	DeletedTopic = "key.deleted"
)

var (
	// ErrInvalidLabel means the label is empty or longer than LabelMaxLength.
	ErrInvalidLabel = errors.New("accounts: label is invalid")
	// ErrInvalidCredential means a key or secret is empty or contains whitespace.
	ErrInvalidCredential = errors.New("accounts: credential is invalid")
	// ErrLimitExceeded means a limit is outside the administrator cap.
	ErrLimitExceeded = errors.New("accounts: limit exceeds the administrator cap")
	// ErrInvalidPolicy means the administrator settings cannot form a key policy.
	ErrInvalidPolicy = errors.New("accounts: administrator limit policy is invalid")
	// ErrInvalidTransition means a status change is not allowed.
	ErrInvalidTransition = errors.New("accounts: invalid status transition")
	// ErrRevoked means the key can no longer be changed.
	ErrRevoked = errors.New("accounts: api key is revoked")
)

// Status is the lifecycle of one API key.
type Status string

// Valid reports whether the status is one of the stored values.
func (s Status) Valid() bool {
	switch s {
	case StatusValid, StatusInvalid, StatusRevoked:
		return true
	default:
		return false
	}
}

// Transition reports whether a key may move from the current status to next.
// Repeating the current status is allowed. Revoked is terminal.
func (s Status) Transition(next Status) error {
	if !s.Valid() || !next.Valid() {
		return ErrInvalidTransition
	}
	if s == next {
		return nil
	}
	switch s {
	case StatusValid:
		if next == StatusInvalid || next == StatusRevoked {
			return nil
		}
	case StatusInvalid:
		if next == StatusValid || next == StatusRevoked {
			return nil
		}
	}
	return ErrInvalidTransition
}

// APIKey is one user's Flickr API key. PublicKey is the full key stored for
// Flickr calls. Responses mask it with MaskPublicKey. The secret is absent.
type APIKey struct {
	ID             int64
	UserID         int64
	Label          string
	PublicKey      string
	PerHourLimit   int
	PerSecondLimit int
	Burst          int
	Status         Status
	CreatedAt      time.Time
}

// MaskedPublicKey returns the display form: the first 6 characters, then a
// bullet for every remaining character. A key of 6 characters or fewer is
// masked completely.
func (k APIKey) MaskedPublicKey() string {
	return MaskPublicKey(k.PublicKey)
}

// WithStatus returns a copy of the key after a legal status transition.
func (k APIKey) WithStatus(next Status) (APIKey, error) {
	if err := k.Status.Transition(next); err != nil {
		return APIKey{}, err
	}
	k.Status = next
	return k, nil
}

// MaskPublicKey masks a stored public key for display.
func MaskPublicKey(public string) string {
	characters := []rune(public)
	if len(characters) == 0 {
		return ""
	}
	if len(characters) <= PublicKeyVisiblePrefix {
		return strings.Repeat("•", len(characters))
	}
	hidden := len(characters) - PublicKeyVisiblePrefix
	return string(characters[:PublicKeyVisiblePrefix]) + strings.Repeat("•", hidden)
}

// SecretAAD is the associated data for sealing a key secret. The platform
// sealer must use this exact value for both Seal and Open: api_keys:<id>.
func SecretAAD(id int64) []byte {
	return []byte(fmt.Sprintf("api_keys:%d", id))
}

// Deleted is the JSON body of DeletedTopic. It identifies the key and owner
// and carries no secret or public key material.
type Deleted struct {
	ID     int64 `json:"id"`
	UserID int64 `json:"user_id"`
}

// Limits are the call budgets stored on one key.
type Limits struct {
	PerHour   int
	PerSecond int
	Burst     int
}

// Bound is one administrator limit. Default is used when the user omits a
// value. Max is the inclusive cap.
type Bound struct {
	Default int
	Max     int
}

// Policy is the administrator cap set applied to key limits.
type Policy struct {
	PerHour   Bound
	PerSecond Bound
	Burst     Bound
}

// PolicyFromSettings builds key limits from application settings.
// per_second_rate and burst are both the default and the cap.
// The hourly cap is FlickrHourlyCeiling. A positive daily_call_limit below
// that ceiling tightens the hourly cap to the daily budget. Zero means the
// daily budget is unlimited, so the Flickr hourly ceiling applies.
func PolicyFromSettings(dailyCallLimit int64, perSecondRate, burst int) (Policy, error) {
	if dailyCallLimit < 0 || perSecondRate < 1 || burst < 1 {
		return Policy{}, ErrInvalidPolicy
	}
	hourly := FlickrHourlyCeiling
	if dailyCallLimit > 0 && dailyCallLimit < int64(FlickrHourlyCeiling) {
		hourly = int(dailyCallLimit)
	}
	return Policy{
		PerHour:   Bound{Default: hourly, Max: hourly},
		PerSecond: Bound{Default: perSecondRate, Max: perSecondRate},
		Burst:     Bound{Default: burst, Max: burst},
	}, nil
}

// Resolve fills omitted create-time limits from the policy and rejects values
// outside the administrator cap.
func (p Policy) Resolve(perHour, perSecond, burst *int) (Limits, error) {
	hour, err := p.PerHour.choose(perHour)
	if err != nil {
		return Limits{}, err
	}
	second, err := p.PerSecond.choose(perSecond)
	if err != nil {
		return Limits{}, err
	}
	size, err := p.Burst.choose(burst)
	if err != nil {
		return Limits{}, err
	}
	return Limits{PerHour: hour, PerSecond: second, Burst: size}, nil
}

func (b Bound) choose(value *int) (int, error) {
	if err := b.valid(); err != nil {
		return 0, err
	}
	chosen := b.Default
	if value != nil {
		chosen = *value
	}
	if chosen < 1 || chosen > b.Max {
		return 0, ErrLimitExceeded
	}
	return chosen, nil
}

// Enforce rejects a limit that is outside this administrator cap.
func (b Bound) Enforce(value int) error {
	if err := b.valid(); err != nil {
		return err
	}
	if value < 1 || value > b.Max {
		return ErrLimitExceeded
	}
	return nil
}

func (b Bound) valid() error {
	if b.Max < 1 || b.Default < 1 || b.Default > b.Max {
		return ErrInvalidPolicy
	}
	return nil
}

// NormalizeLabel trims a label and rejects an empty or oversized value.
func NormalizeLabel(label string) (string, error) {
	trimmed := strings.TrimSpace(label)
	if trimmed == "" || len([]rune(trimmed)) > LabelMaxLength || strings.ContainsFunc(trimmed, unicode.IsControl) {
		return "", ErrInvalidLabel
	}
	return trimmed, nil
}

// NormalizeCredential trims a Flickr key or secret and rejects empty values,
// whitespace, and control characters.
func NormalizeCredential(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > CredentialMaxLength || strings.ContainsFunc(trimmed, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}) {
		return "", ErrInvalidCredential
	}
	return trimmed, nil
}
