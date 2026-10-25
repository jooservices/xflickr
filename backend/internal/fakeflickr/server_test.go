package fakeflickr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
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
	info, err := c.PeopleGetInfo(ctx, creds, "10000000@N00")
	if err != nil || info.NSID != "10000000@N00" || string(info.Username) != "seed-user" {
		t.Fatalf("person info=%+v err=%v", info, err)
	}
	contacts, err := c.ContactsGetPublicList(ctx, creds, "10000000@N00", opts)
	if err != nil || len(contacts.Items) != 2 || contacts.Items[0].NSID != "10000001@N00" {
		t.Fatalf("root contacts=%+v err=%v", contacts, err)
	}
	frontier := []string{"10000000@N00"}
	levels := [][]string{{"10000000@N00"}, {"10000001@N00", "10000002@N00"}, {"10000003@N00", "10000004@N00"}, {"10000005@N00", "10000006@N00"}}
	for depth := 1; depth <= 3; depth++ {
		next := []string{}
		for _, nsid := range frontier {
			page, e := c.ContactsGetPublicList(ctx, creds, nsid, opts)
			if e != nil {
				t.Fatalf("contacts depth %d for %s: %v", depth, nsid, e)
			}
			for _, contact := range page.Items {
				next = append(next, contact.NSID)
			}
		}
		if !sameIDs(next, levels[depth]) {
			t.Fatalf("contact depth %d=%v want %v", depth, next, levels[depth])
		}
		frontier = next
	}
	favoritePeople := []string{"10000001@N00", "10000003@N00", "10000005@N00"}
	wantOwners := []string{"10000000@N00", "10000001@N00", "10000003@N00"}
	for i, nsid := range favoritePeople {
		page, e := c.FavoritesGetList(ctx, creds, nsid, opts)
		if e != nil || len(page.Items) != 1 || page.Items[0].Owner != wantOwners[i] {
			t.Fatalf("favorite depth %d for %s = %+v err=%v", i+1, nsid, page.Items, e)
		}
	}
}

func sameIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]bool{}
	for _, id := range got {
		seen[id] = true
	}
	for _, id := range want {
		if !seen[id] {
			return false
		}
	}
	return true
}

func TestMissingEntitiesForwardedURLsAndDistinctImages(t *testing.T) {
	server := testServer(t, NewScenarios(ScenarioConfig{}))
	client := flickr.NewClient(server.URL+"/services/rest", server.Client())
	creds, ctx := flickr.Credentials{APIKey: "key"}, context.Background()
	if _, err := client.PeopleGetInfo(ctx, creds, "missing-user"); !errors.Is(err, flickr.ErrNotFound) {
		t.Fatalf("unknown user error=%v", err)
	}
	if _, err := client.FavoritesGetList(ctx, creds, "missing-user", flickr.ListOptions{}); !errors.Is(err, flickr.ErrNotFound) {
		t.Fatalf("unknown favorites user error=%v", err)
	}
	if _, err := client.PhotosGetInfo(ctx, creds, "missing-photo", ""); !errors.Is(err, flickr.ErrNotFound) {
		t.Fatalf("unknown photo error=%v", err)
	}
	if _, err := client.PhotosetsGetPhotos(ctx, creds, "10000000@N00", "missing-set", flickr.ListOptions{}); err == nil {
		t.Fatal("unknown photoset accepted")
	}
	if _, err := client.PhotosetsGetPhotos(ctx, creds, "10000001@N00", "72177720000000001", flickr.ListOptions{}); err == nil {
		t.Fatal("photoset returned for a different owner")
	}
	if _, err := client.GalleriesGetPhotos(ctx, creds, "missing-gallery", flickr.ListOptions{}); err == nil {
		t.Fatal("unknown gallery accepted")
	}
	if _, err := client.GroupsGetInfo(ctx, creds, "missing-group"); err == nil {
		t.Fatal("unknown group accepted")
	}
	groups, err := client.PeopleGetGroups(ctx, creds, "10000003@N00")
	if err != nil || len(groups) != 0 {
		t.Fatalf("non-member groups=%v err=%v", groups, err)
	}

	post := func(values url.Values) map[string]any {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/services/rest", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-Forwarded-Host", "fakeflickr:8088")
		res, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		var body map[string]any
		if e := json.NewDecoder(res.Body).Decode(&body); e != nil {
			t.Fatal(e)
		}
		return body
	}
	person := post(url.Values{"method": {"flickr.people.getInfo"}, "user_id": {"10000000@N00"}})["person"].(map[string]any)
	if person["profileurl"].(map[string]any)["_content"] != "https://fakeflickr:8088/people/10000000@N00" {
		t.Fatalf("person urls=%v", person)
	}
	photo := post(url.Values{"method": {"flickr.photos.getInfo"}, "photo_id": {"52000000001"}})["photo"].(map[string]any)
	if got := photo["urls"].(map[string]any)["url"].([]any)[0].(map[string]any)["_content"]; got != "https://fakeflickr:8088/photo/52000000001" {
		t.Fatalf("photo page URL=%v", got)
	}
	sizes := post(url.Values{"method": {"flickr.photos.getSizes"}, "photo_id": {"52000000001"}})["sizes"].(map[string]any)["size"].([]any)
	if got := sizes[0].(map[string]any)["source"]; got != "https://fakeflickr:8088/images/52000000001.jpg" {
		t.Fatalf("size URL=%v", got)
	}
	photos := post(url.Values{"method": {"flickr.people.getPhotos"}, "user_id": {"10000000@N00"}})["photos"].(map[string]any)["photo"].([]any)
	if got := photos[0].(map[string]any)["url_o"]; got != "https://fakeflickr:8088/images/52000000001.jpg" {
		t.Fatalf("CDN URL=%v", got)
	}
	fetch := func(id string) []byte {
		res, e := server.Client().Get(server.URL + "/images/" + id + ".jpg")
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		var b bytes.Buffer
		_, _ = b.ReadFrom(res.Body)
		return b.Bytes()
	}
	first, repeat, second := fetch("52000000001"), fetch("52000000001"), fetch("52000000002")
	if sha256.Sum256(first) != sha256.Sum256(repeat) || sha256.Sum256(first) == sha256.Sum256(second) {
		t.Fatal("generated images are not stable and photo-specific")
	}
}

func TestOAuthAndImageEndpoints(t *testing.T) {
	server := testServer(t, NewScenarios(ScenarioConfig{}))
	flow := oauth.NewFlow("key", "secret", server.Client())
	flow.SetEndpoints(server.URL+"/services/oauth/request_token", server.URL+"/services/oauth/authorize", server.URL+"/services/oauth/access_token")
	token, secret, err := flow.RequestToken(context.Background(), "http://localhost/callback")
	if err != nil || !strings.HasPrefix(token, "request-token-") || secret != "request-secret" {
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

func TestRejectedRequestTokenDoesNotStoreCallback(t *testing.T) {
	server := testServer(t, NewScenarios(ScenarioConfig{BanIPs: []string{"192.0.2.8"}}))
	form := url.Values{"oauth_callback": {"http://localhost/callback"}, "api_key": {"key"}}
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/services/oauth/request_token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Client-IP", "192.0.2.8")
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("rejected request token status=%d", res.StatusCode)
	}
	res, err = server.Client().Get(server.URL + "/services/oauth/authorize?oauth_token=request-token-1")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("rejected token callback status=%d", res.StatusCode)
	}
}

func TestScenarioSwitchesAndAdminEndpoint(t *testing.T) {
	server := testServer(t, NewScenarios(ScenarioConfig{}))
	callKey := func(key, ip, token string) *http.Response {
		form := url.Values{"method": {"flickr.test.echo"}, "api_key": {key}, "oauth_token": {token}, "format": {"json"}}
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
	call := func(ip, token string) *http.Response { return callKey("key", ip, token) }
	admin := func(body string) int {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/__admin/scenarios", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	if status := admin(`{"ban_ips":["192.0.2.4"],"revoked_tokens":["revoked"],"rate_limit_after":1}`); status != 204 {
		t.Fatalf("configure status=%d", status)
	}
	res := call("192.0.2.4", "")
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("ban status=%d", res.StatusCode)
	}
	res = call("", "revoked")
	var revoked map[string]any
	_ = json.NewDecoder(res.Body).Decode(&revoked)
	res.Body.Close()
	if res.StatusCode != 200 || revoked["stat"] != "fail" || revoked["code"] != float64(98) {
		t.Fatalf("revocation status=%d body=%v", res.StatusCode, revoked)
	}
	res = call("", "")
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("first limited request status=%d", res.StatusCode)
	}
	res = call("", "")
	res.Body.Close()
	if res.StatusCode != 429 {
		t.Fatalf("rate limit status=%d", res.StatusCode)
	}
	if status := admin(`{"rate_limit_after":1}`); status != 204 {
		t.Fatalf("reconfigure status=%d", status)
	}
	res = call("", "")
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("admin config did not reset counter: %d", res.StatusCode)
	}
	res = call("", "")
	res.Body.Close()
	if res.StatusCode != 429 {
		t.Fatalf("post-admin rate limit status=%d", res.StatusCode)
	}
	if status := admin(`{"rate_limit_after":0}`); status != 204 {
		t.Fatalf("disable scenario status=%d", status)
	}
	res = call("", "")
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("disabled rate limit status=%d", res.StatusCode)
	}
	if status := admin(`{"quota_per_hour":1}`); status != 204 {
		t.Fatalf("quota config status=%d", status)
	}
	res = callKey("key-one", "", "")
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("first key quota call=%d", res.StatusCode)
	}
	res = callKey("key-one", "", "")
	res.Body.Close()
	if res.StatusCode != 429 {
		t.Fatalf("exhausted key quota call=%d", res.StatusCode)
	}
	res = callKey("key-two", "", "")
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("quota leaked across API keys: %d", res.StatusCode)
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
}

func TestScenarioLatencyCancellationAndEnvironment(t *testing.T) {
	server := testServer(t, NewScenarios(ScenarioConfig{LatencyMS: 60}))
	started := time.Now()
	res, err := server.Client().PostForm(server.URL+"/services/rest", url.Values{"method": {"flickr.test.echo"}, "api_key": {"key"}})
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if time.Since(started) < 50*time.Millisecond {
		t.Fatal("latency scenario returned before the configured delay")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/services/rest", strings.NewReader("method=flickr.test.echo&api_key=cancel"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, err = server.Client().Do(req)
	if err == nil {
		t.Fatal("expected canceled request to stop during injected latency")
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
