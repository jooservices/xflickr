package fakeflickr

import (
	"context"
	"encoding/json"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/pkg/flickr"
	"github.com/jooservices/xflickr/pkg/flickr/oauth"
)

func testServer(t *testing.T, scenarios *Scenarios) *httptest.Server {
	t.Helper()
	dataset, err := LoadDataset("")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(NewServer(dataset, scenarios))
	t.Cleanup(httpServer.Close)
	return httpServer
}

func TestLoadDataset(t *testing.T) {
	dataset, err := LoadDataset("")
	if err != nil || len(dataset.People) < 7 || len(dataset.Photos) < 3 || len(dataset.Favorites) < 3 || len(dataset.Albums) == 0 || len(dataset.Contacts) < 6 || len(dataset.Groups) == 0 || len(dataset.Galleries) == 0 {
		t.Fatalf("dataset=%+v err=%v", dataset, err)
	}
	if _, err := LoadDataset("missing-seed.yaml"); err == nil {
		t.Fatal("expected missing seed error")
	}
}

func TestFakeFlickrContractForTypedMethods(t *testing.T) {
	server := testServer(t, NewScenarios(ScenarioConfig{}))
	c := flickr.NewClient(server.URL+"/services/rest", server.Client())
	ctx, creds, opts := context.Background(), flickr.Credentials{APIKey: "public-test-key"}, flickr.ListOptions{PerPage: 10}
	checks := []struct {
		name string
		call func() error
	}{
		{"test.echo", func() error {
			v, e := c.TestEcho(ctx, creds, map[string]string{"custom": "value"})
			if e == nil && v["custom"] != "value" {
				t.Errorf("echo=%v", v)
			}
			return e
		}},
		{"test.login", func() error { _, e := c.TestLogin(ctx, creds); return e }},
		{"people.getInfo", func() error { _, e := c.PeopleGetInfo(ctx, creds, "10000000@N00"); return e }},
		{"people.getPhotos", func() error { _, e := c.PeopleGetPhotos(ctx, creds, "10000000@N00", opts); return e }},
		{"people.getPublicPhotos", func() error { _, e := c.PeopleGetPublicPhotos(ctx, creds, "10000000@N00", opts); return e }},
		{"favorites.getList", func() error { _, e := c.FavoritesGetList(ctx, creds, "10000000@N00", opts); return e }},
		{"favorites.getPublicList", func() error { _, e := c.FavoritesGetPublicList(ctx, creds, "10000000@N00", opts); return e }},
		{"contacts.getList", func() error { _, e := c.ContactsGetList(ctx, creds, opts); return e }},
		{"contacts.getPublicList", func() error { _, e := c.ContactsGetPublicList(ctx, creds, "10000000@N00", opts); return e }},
		{"photos.getContactsPhotos", func() error {
			_, e := c.PhotosGetContactsPhotos(ctx, creds, flickr.ContactsPhotosOptions{Count: 10})
			return e
		}},
		{"photos.getSizes", func() error { _, e := c.PhotosGetSizes(ctx, creds, "52000000001"); return e }},
		{"photos.getInfo", func() error { _, e := c.PhotosGetInfo(ctx, creds, "52000000001", ""); return e }},
		{"photos.getExif", func() error { _, e := c.PhotosGetExif(ctx, creds, "52000000001", ""); return e }},
		{"photos.getAllContexts", func() error { _, e := c.PhotosGetAllContexts(ctx, creds, "52000000001"); return e }},
		{"photos.comments.getList", func() error { _, e := c.PhotosCommentsGetList(ctx, creds, "52000000001", opts); return e }},
		{"photos.getFavorites", func() error { _, e := c.PhotosGetFavorites(ctx, creds, "52000000001", opts); return e }},
		{"photos.people.getList", func() error { _, e := c.PhotosPeopleGetList(ctx, creds, "52000000001"); return e }},
		{"photosets.getList", func() error { _, e := c.PhotosetsGetList(ctx, creds, "10000000@N00", opts); return e }},
		{"photosets.getPhotos", func() error {
			_, e := c.PhotosetsGetPhotos(ctx, creds, "10000000@N00", "72177720000000001", opts)
			return e
		}},
		{"galleries.getList", func() error { _, e := c.GalleriesGetList(ctx, creds, "10000000@N00", opts); return e }},
		{"galleries.getPhotos", func() error { _, e := c.GalleriesGetPhotos(ctx, creds, "72157600000000001", opts); return e }},
		{"people.getGroups", func() error { _, e := c.PeopleGetGroups(ctx, creds, "10000000@N00"); return e }},
		{"people.getPublicGroups", func() error { _, e := c.PeopleGetPublicGroups(ctx, creds, "10000000@N00"); return e }},
		{"groups.getInfo", func() error { _, e := c.GroupsGetInfo(ctx, creds, "90000000@N00"); return e }},
		{"groups.pools.getPhotos", func() error { _, e := c.GroupsPoolsGetPhotos(ctx, creds, "90000000@N00", opts); return e }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOAuthAndImageEndpoints(t *testing.T) {
	server := testServer(t, NewScenarios(ScenarioConfig{}))
	flow := oauth.NewFlow("key", "secret", server.Client())
	flow.SetEndpoints(server.URL+"/services/oauth/request_token", server.URL+"/services/oauth/authorize", server.URL+"/services/oauth/access_token")
	token, secret, err := flow.RequestToken(context.Background(), "http://localhost/callback")
	if err != nil || token != "request-token" || secret != "request-secret" {
		t.Fatalf("request token=%q secret=%q err=%v", token, secret, err)
	}
	authorizeURL, err := flow.AuthorizeURL(token, "read")
	if err != nil {
		t.Fatal(err)
	}
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	redirect, err := noRedirect.Get(authorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer redirect.Body.Close()
	location, err := url.Parse(redirect.Header.Get("Location"))
	if err != nil || location.Host != "localhost" || location.Query().Get("oauth_verifier") != "fake-verifier" {
		t.Fatalf("authorization redirect=%v err=%v", location, err)
	}
	access, accessSecret, nsid, _, err := flow.AccessToken(context.Background(), "verifier")
	if err != nil || access != "access-token" || accessSecret != "access-secret" || nsid != "10000000@N00" {
		t.Fatalf("access=%q %q %q err=%v", access, accessSecret, nsid, err)
	}
	r, err := server.Client().Get(server.URL + "/images/52000000001.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("image status=%d headers=%v", r.StatusCode, r.Header)
	}
	img, err := jpeg.Decode(r.Body)
	if err != nil || img.Bounds().Dx() != 320 || img.Bounds().Dy() != 240 {
		t.Fatalf("decoded image=%v err=%v", img, err)
	}
	missing, err := server.Client().Get(server.URL + "/images/missing.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer missing.Body.Close()
	if missing.StatusCode != 404 {
		t.Fatalf("missing status=%d", missing.StatusCode)
	}
}

func TestScenarioSwitchesAndAdminEndpoint(t *testing.T) {
	server := testServer(t, NewScenarios(ScenarioConfig{QuotaPerHour: 1, BanIPs: []string{"192.0.2.4"}, RevokedTokens: []string{"revoked"}}))
	call := func(ip, token string) *http.Response {
		form := url.Values{"method": {"flickr.test.echo"}, "api_key": {"key"}, "oauth_token": {token}, "format": {"json"}}
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/services/rest", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if ip != "" {
			req.Header.Set("X-Client-IP", ip)
		}
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	res := call("192.0.2.4", "")
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("ban status=%d", res.StatusCode)
	}
	res = call("", "revoked")
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("revocation is Flickr error envelope, status=%d", res.StatusCode)
	}
	server.Config.Handler.(*Server).scenarios.Configure(ScenarioConfig{QuotaPerHour: 1})
	res = call("", "")
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("first request status=%d", res.StatusCode)
	}
	res = call("", "")
	res.Body.Close()
	if res.StatusCode != 429 {
		t.Fatalf("quota status=%d", res.StatusCode)
	}
	admin := func(body string) int {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/__admin/scenarios", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if status := admin(`{"rate_limit_after":1,"latency_ms":1}`); status != 204 {
		t.Fatalf("configure status=%d", status)
	}
	if admin(`{`) != 400 {
		t.Fatal("malformed admin config accepted")
	}
	get, err := server.Client().Get(server.URL + "/__admin/scenarios")
	if err != nil {
		t.Fatal(err)
	}
	get.Body.Close()
	if get.StatusCode != 405 {
		t.Fatalf("admin GET status=%d", get.StatusCode)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = ctx
}

func TestScenarioLatencyAndEnvironment(t *testing.T) {
	s := NewScenarios(ScenarioConfig{LatencyMS: 1})
	server := testServer(t, s)
	started := time.Now()
	res, err := server.Client().PostForm(server.URL+"/services/rest", url.Values{"method": {"flickr.test.echo"}, "api_key": {"key"}})
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if time.Since(started) < time.Millisecond {
		t.Fatal("latency scenario did not delay")
	}
	t.Setenv("FAKEFLICKR_QUOTA_PER_HOUR", "7")
	t.Setenv("FAKEFLICKR_RATE_LIMIT_AFTER", "3")
	t.Setenv("FAKEFLICKR_LATENCY_MS", "9")
	t.Setenv("FAKEFLICKR_BAN_IP", "192.0.2.9")
	t.Setenv("FAKEFLICKR_REVOKED_TOKEN", "token-x")
	c := ScenarioConfigFromEnvironment()
	if c.QuotaPerHour != 7 || c.RateLimitAfter != 3 || c.LatencyMS != 9 || len(c.BanIPs) != 1 || len(c.RevokedTokens) != 1 {
		t.Fatalf("env config=%+v", c)
	}
	if envInt("missing") != 0 {
		t.Fatal("missing integer should be zero")
	}
	t.Setenv("FAKEFLICKR_QUOTA_PER_HOUR", "-3")
	if envInt("FAKEFLICKR_QUOTA_PER_HOUR") != 0 {
		t.Fatal("negative integer should be zero")
	}
}

func TestUnknownMethodsAndWrongMethods(t *testing.T) {
	server := testServer(t, NewScenarios(ScenarioConfig{}))
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req, _ := http.NewRequest(method, server.URL+"/services/rest", strings.NewReader("method=bad"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if method == http.MethodGet && res.StatusCode != 405 {
			t.Fatalf("GET status=%d", res.StatusCode)
		}
	}
	for _, name := range []string{"not-flickr", "flickr.unknown"} {
		res, err := server.Client().PostForm(server.URL+"/services/rest", url.Values{"method": {name}, "api_key": {"k"}})
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.NewDecoder(res.Body).Decode(&body)
		res.Body.Close()
		if body["stat"] != "fail" {
			t.Fatalf("method %q response=%v", name, body)
		}
	}
}
