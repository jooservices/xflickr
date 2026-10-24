package flickr

import (
	"io"
	"net/http"
	"net/http/httptest"
)

// NewFakeFlickr serves flickr.test.echo. Keys in validKeys succeed. Every
// other key is rejected with Flickr error code 100. The secret is never read.
func NewFakeFlickr(validKeys ...string) *httptest.Server {
	allowed := make(map[string]struct{}, len(validKeys))
	for _, key := range validKeys {
		allowed[key] = struct{}{}
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if request.Form.Get("method") != echoMethod {
			http.Error(w, "unexpected method", http.StatusInternalServerError)
			return
		}
		if request.Form.Get("secret") != "" || request.Form.Get("api_sig") != "" {
			http.Error(w, "secret material is not accepted", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, ok := allowed[request.Form.Get("api_key")]; !ok {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"stat":"fail","code":100,"message":"Invalid API Key (Key not found)"}`)
			return
		}
		_, _ = io.WriteString(w, `{"stat":"ok"}`)
	}))
}
