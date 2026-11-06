package rest

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

func ETag(representation []byte) string {
	sum := sha256.Sum256(representation)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

// RequireIfMatch requires a strong validator; an empty current tag means the
// resource does not exist. Consumers must compare and update atomically.
func RequireIfMatch(r *http.Request, current string) error {
	for _, line := range r.Header.Values("If-Match") {
		for _, candidate := range strings.Split(line, ",") {
			candidate = strings.TrimSpace(candidate)
			if current != "" && (candidate == "*" || (!strings.HasPrefix(current, "W/") && candidate == current)) {
				return nil
			}
		}
	}
	return NewProblem("precondition-failed", "A current If-Match validator is required", nil)
}
