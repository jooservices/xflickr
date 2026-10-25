package roles

import (
	"testing"

	"github.com/jooservices/xflickr/internal/platform/config"
)

func TestFlickrUpstreamDefaultsToFlickr(t *testing.T) {
	cfg := config.Config{}
	if got := flickrRESTURL(cfg); got != "" {
		t.Errorf("REST URL = %q, want empty", got)
	}
	if got := flickrOAuthEndpoints(cfg); got.RequestToken != "" || got.Authorize != "" || got.AccessToken != "" {
		t.Errorf("OAuth endpoints = %+v, want empty", got)
	}
}

func TestFlickrUpstreamRedirectsRESTAndOAuth(t *testing.T) {
	cfg := config.Config{Flickr: config.FlickrConfig{UpstreamURL: "http://127.0.0.1:8088"}}
	if got := flickrRESTURL(cfg); got != "http://127.0.0.1:8088/services/rest" {
		t.Errorf("REST URL = %q", got)
	}
	got := flickrOAuthEndpoints(cfg)
	if got.RequestToken != "http://127.0.0.1:8088/services/oauth/request_token" ||
		got.Authorize != "http://127.0.0.1:8088/services/oauth/authorize" ||
		got.AccessToken != "http://127.0.0.1:8088/services/oauth/access_token" {
		t.Errorf("OAuth endpoints = %+v", got)
	}
}

func TestAPIAddressFollowsHTTPPort(t *testing.T) {
	for _, c := range []struct{ value, want string }{
		{"", ":8080"}, {"9123", ":9123"}, {" 9124 ", ":9124"}, {"abc", ":8080"}, {"0", ":8080"}, {"70000", ":8080"},
	} {
		t.Setenv("HTTP_PORT", c.value)
		if got := apiAddress(); got != c.want {
			t.Errorf("HTTP_PORT=%q: address = %q, want %q", c.value, got, c.want)
		}
	}
}
