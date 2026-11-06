package rest

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
)

// ReadMergePatch preserves null and nested values for the resource's merge
// semantics. Only writable top-level fields belong in allowed; nested schemas
// remain the consumer's responsibility.
func ReadMergePatch(r *http.Request, maxBytes int64, allowed ...string) (map[string]json.RawMessage, error) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/merge-patch+json" {
		return nil, NewProblem("unsupported-media-type", "Use application/merge-patch+json", nil)
	}
	if maxBytes <= 0 {
		return nil, NewProblem("malformed-request", "Invalid body limit", nil)
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil || int64(len(raw)) > maxBytes {
		return nil, NewProblem("malformed-request", "Request body cannot be read or exceeds its limit", nil)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, NewProblem("malformed-request", "Expected a JSON object", nil)
	}
	permitted := make(map[string]bool, len(allowed))
	for _, field := range allowed {
		permitted[field] = true
	}
	invalid := FieldErrors{}
	for field := range fields {
		if !permitted[field] {
			invalid[field] = []string{"Field is not writable"}
		}
	}
	if len(invalid) > 0 {
		return nil, NewProblem("validation-error", "Unknown or read-only fields", invalid)
	}
	return fields, nil
}
