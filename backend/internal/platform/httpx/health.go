package httpx

import (
	"encoding/json"
	"net/http"
)

// NewHealthHandler creates the process health endpoint with its build version.
func NewHealthHandler(version string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Status  string `json:"status"`
			Version string `json:"version"`
		}{Status: "ok", Version: version})
	})
	return mux
}
