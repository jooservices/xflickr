package rest

import (
	"encoding/base64"
	"net/url"
	"strconv"
)

const (
	DefaultLimit = 50
	MaxLimit     = 100
)

func EncodeCursor(value []byte) string { return base64.RawURLEncoding.EncodeToString(value) }
func DecodeCursor(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > 5462 {
		return nil, NewProblem("validation-error", "Invalid cursor", FieldErrors{"cursor": {"Invalid cursor"}})
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(decoded) == 0 || len(decoded) > 4096 {
		return nil, NewProblem("validation-error", "Invalid cursor", FieldErrors{"cursor": {"Invalid cursor"}})
	}
	return decoded, nil
}

func ParseLimit(value string) (int, error) {
	if value == "" {
		return DefaultLimit, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > MaxLimit {
		return 0, NewProblem("validation-error", "Invalid limit", FieldErrors{"limit": {"Must be between 1 and 100"}})
	}
	return limit, nil
}

type Pagination struct {
	Limit      int    `json:"limit"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor"`
}

// PageMeta preserves filters and uses only a real next cursor supplied by the
// repository's deterministic ordering (including its unique tiebreaker).
func PageMeta(self *url.URL, limit int, nextCursor string) map[string]any {
	links := map[string]string{"self": self.String()}
	if nextCursor != "" {
		next := *self
		query := next.Query()
		query.Set("cursor", nextCursor)
		query.Set("limit", strconv.Itoa(limit))
		next.RawQuery = query.Encode()
		links["next"] = next.String()
	}
	return map[string]any{"pagination": Pagination{limit, nextCursor != "", nextCursor}, "links": links}
}
