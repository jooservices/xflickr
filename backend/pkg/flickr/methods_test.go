package flickr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fixtureClient serves testdata/<method>.json for every call and records the
// last form it received.
func fixtureClient(t *testing.T) (*Client, *map[string]string) {
	t.Helper()
	last := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		for key := range last {
			delete(last, key)
		}
		for key := range r.Form {
			last[key] = r.Form.Get(key)
		}
		name := r.Form.Get("method")[len("flickr."):]
		body, err := os.ReadFile(filepath.Join("testdata", name+".json"))
		if err != nil {
			t.Errorf("fixture for %s: %v", name, err)
			http.Error(w, "no fixture", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return NewClient(server.URL, server.Client()), &last
}

func assertPhotoPage(t *testing.T, page Page[Photo], err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if page.Page != 1 || page.Pages != 3 || page.Total != 1250 || page.PerPage != 500 || len(page.Items) != 1 {
		t.Fatalf("page meta = %+v", page)
	}
	p := page.Items[0]
	if p.ID != "52000000001" || p.Server != "65535" || p.Farm != 66 || p.DateUpload != 1700000000 ||
		p.LastUpdate != 1700000100 || p.Views != 42 || p.OWidth != 4000 || p.OHeight != 3000 ||
		p.Description != "A fake description" || p.License != "0" || p.Media != "photo" || p.URLO == "" || p.URLSq == "" {
		t.Fatalf("photo = %+v", p)
	}
}

func TestTypedMethodsDecodeFixtures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	creds := Credentials{APIKey: "k"}
	opts := ListOptions{PerPage: 500}

	tests := map[string]func(t *testing.T, c *Client){
		"photos.getSizes": func(t *testing.T, c *Client) {
			sizes, err := c.PhotosGetSizes(ctx, creds, "52000000001")
			if err != nil || len(sizes) != 3 || sizes[0].Label != "Square" || sizes[0].Width != 75 || sizes[0].Height != 75 || sizes[0].Source == "" || sizes[0].Media != "photo" {
				t.Fatalf("sizes = %+v, err = %v", sizes, err)
			}
		},
		"photos.getInfo": func(t *testing.T, c *Client) {
			photo, err := c.PhotosGetInfo(ctx, creds, "52000000001", "abc123def4")
			if err != nil || photo.ID != "52000000001" || photo.Owner.NSID != "00000000@N00" || photo.Title != "Sample photo" || photo.Description != "A fake description" || len(photo.Tags.Items) != 1 || len(photo.URLs.Items) != 1 {
				t.Fatalf("photo = %+v, err = %v", photo, err)
			}
		},
		"photos.getExif": func(t *testing.T, c *Client) {
			exif, err := c.PhotosGetExif(ctx, creds, "52000000001", "")
			if err != nil || exif.ID != "52000000001" || exif.Camera != "Fake Camera" || len(exif.Entries) != 1 || exif.Entries[0].Clean != "Fake Camera" {
				t.Fatalf("exif = %+v, err = %v", exif, err)
			}
		},
		"photos.getAllContexts": func(t *testing.T, c *Client) {
			contexts, err := c.PhotosGetAllContexts(ctx, creds, "52000000001")
			if err != nil || len(contexts.Sets) != 1 || contexts.Sets[0].ID != "72177720000000001" || len(contexts.Pools) != 1 {
				t.Fatalf("contexts = %+v, err = %v", contexts, err)
			}
		},
		"photos.comments.getList": func(t *testing.T, c *Client) {
			comments, err := c.PhotosCommentsGetList(ctx, creds, "52000000001", ListOptions{Page: 1, PerPage: 100})
			if err != nil || len(comments.Items) != 1 || comments.Total != 1 || comments.Items[0].Text != "Nice photo" {
				t.Fatalf("comments = %+v, err = %v", comments, err)
			}
		},
		"photos.getFavorites": func(t *testing.T, c *Client) {
			favorites, err := c.PhotosGetFavorites(ctx, creds, "52000000001", ListOptions{})
			if err != nil || len(favorites.Items) != 1 || favorites.Items[0].NSID != "00000001@N00" || favorites.Items[0].FaveDate != "1700000400" {
				t.Fatalf("favorites = %+v, err = %v", favorites, err)
			}
		},
		"photos.people.getList": func(t *testing.T, c *Client) {
			people, err := c.PhotosPeopleGetList(ctx, creds, "52000000001")
			if err != nil || len(people) != 1 || people[0].NSID != "00000001@N00" || people[0].IsPro != 0 {
				t.Fatalf("people = %+v, err = %v", people, err)
			}
		},
		"people.getInfo": func(t *testing.T, c *Client) {
			p, err := c.PeopleGetInfo(ctx, creds, "00000000@N00")
			if err != nil || p.NSID != "00000000@N00" || p.Username != "fakeuser" || p.IsPro != 1 || p.Photos.Count != "1234" || p.PathAlias != "fakeuser" {
				t.Fatalf("person = %+v, err = %v", p, err)
			}
		},
		"people.getPhotos": func(t *testing.T, c *Client) {
			page, err := c.PeopleGetPhotos(ctx, creds, "x", opts)
			assertPhotoPage(t, page, err)
		},
		"people.getPublicPhotos": func(t *testing.T, c *Client) {
			page, err := c.PeopleGetPublicPhotos(ctx, creds, "x", opts)
			assertPhotoPage(t, page, err)
		},
		"favorites.getList": func(t *testing.T, c *Client) {
			page, err := c.FavoritesGetList(ctx, creds, "", opts)
			assertPhotoPage(t, page, err)
			if page.Items[0].DateFaved != 1700000200 {
				t.Fatalf("date_faved = %d", page.Items[0].DateFaved)
			}
		},
		"favorites.getPublicList": func(t *testing.T, c *Client) {
			page, err := c.FavoritesGetPublicList(ctx, creds, "x", opts)
			assertPhotoPage(t, page, err)
		},
		"photosets.getList": func(t *testing.T, c *Client) {
			page, err := c.PhotosetsGetList(ctx, creds, "x", opts)
			if err != nil || page.Total != 3 || page.Pages != 2 || page.PerPage != 500 || len(page.Items) != 1 {
				t.Fatalf("page = %+v, err = %v", page, err)
			}
			if s := page.Items[0]; s.ID != "72177720000000001" || s.Title != "Fake album" || s.Photos != 12 || s.Videos != 1 || s.DateCreate != 1700000000 {
				t.Fatalf("photoset = %+v", s)
			}
		},
		"photosets.getPhotos": func(t *testing.T, c *Client) {
			page, err := c.PhotosetsGetPhotos(ctx, creds, "x", "72177720000000001", opts)
			if err != nil || page.Total != 1 || page.PerPage != 500 || len(page.Items) != 1 || page.Items[0].ID != "52000000001" {
				t.Fatalf("page = %+v, err = %v", page, err)
			}
		},
		"galleries.getList": func(t *testing.T, c *Client) {
			page, err := c.GalleriesGetList(ctx, creds, "x", opts)
			if err != nil || len(page.Items) != 1 {
				t.Fatalf("page = %+v, err = %v", page, err)
			}
			if g := page.Items[0]; g.GalleryID != "72157600000000001" || g.CountPhotos != 5 || g.CountTotal != 5 || g.Title != "Fake gallery" || g.CountComments != 2 {
				t.Fatalf("gallery = %+v", g)
			}
		},
		"galleries.getPhotos": func(t *testing.T, c *Client) {
			page, err := c.GalleriesGetPhotos(ctx, creds, "g", opts)
			assertPhotoPage(t, page, err)
		},
		"people.getGroups": func(t *testing.T, c *Client) {
			groups, err := c.PeopleGetGroups(ctx, creds, "x")
			if err != nil || len(groups) != 1 || groups[0].NSID != "90000000@N00" || groups[0].Admin != 1 || groups[0].Members != 150 || groups[0].PoolCount != 2500 {
				t.Fatalf("groups = %+v, err = %v", groups, err)
			}
		},
		"people.getPublicGroups": func(t *testing.T, c *Client) {
			groups, err := c.PeopleGetPublicGroups(ctx, creds, "x")
			if err != nil || len(groups) != 1 || groups[0].Name != "Fake group" {
				t.Fatalf("groups = %+v, err = %v", groups, err)
			}
		},
		"groups.getInfo": func(t *testing.T, c *Client) {
			g, err := c.GroupsGetInfo(ctx, creds, "90000000@N00")
			if err != nil || g.Name != "Fake group" || g.Members != "150" || g.PoolCount != "2500" {
				t.Fatalf("group = %+v, err = %v", g, err)
			}
		},
		"groups.pools.getPhotos": func(t *testing.T, c *Client) {
			page, err := c.GroupsPoolsGetPhotos(ctx, creds, "g", opts)
			assertPhotoPage(t, page, err)
		},
		"contacts.getList": func(t *testing.T, c *Client) {
			page, err := c.ContactsGetList(ctx, creds, opts)
			if err != nil || page.Total != 1001 || page.PerPage != 1000 || len(page.Items) != 1 {
				t.Fatalf("page = %+v, err = %v", page, err)
			}
			if c := page.Items[0]; c.NSID != "00000001@N00" || c.Friend != 1 || c.Family != 0 || c.Username != "fakefriend" {
				t.Fatalf("contact = %+v", c)
			}
		},
		"contacts.getPublicList": func(t *testing.T, c *Client) {
			page, err := c.ContactsGetPublicList(ctx, creds, "x", opts)
			if err != nil || len(page.Items) != 1 || page.Items[0].RealName != "Fake Friend" {
				t.Fatalf("page = %+v, err = %v", page, err)
			}
		},
		"photos.getContactsPhotos": func(t *testing.T, c *Client) {
			photos, err := c.PhotosGetContactsPhotos(ctx, creds, ContactsPhotosOptions{Count: 10, JustFriends: true})
			if err != nil || len(photos) != 1 || photos[0].OwnerName != "fakeuser" {
				t.Fatalf("photos = %+v, err = %v", photos, err)
			}
		},
		"test.login": func(t *testing.T, c *Client) {
			u, err := c.TestLogin(ctx, creds)
			if err != nil || u.ID != "00000000@N00" || u.Username != "fakeuser" {
				t.Fatalf("user = %+v, err = %v", u, err)
			}
		},
		"test.echo": func(t *testing.T, c *Client) {
			echoed, err := c.TestEcho(ctx, creds, map[string]string{"foo": "bar"})
			if err != nil || echoed["foo"] != "bar" || echoed["method"] != "flickr.test.echo" {
				t.Fatalf("echo = %+v, err = %v", echoed, err)
			}
		},
	}
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(tests) {
		t.Fatalf("%d fixtures but %d method tests", len(entries), len(tests))
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client, _ := fixtureClient(t)
			run(t, client)
		})
	}
}

func TestLargestAllowed(t *testing.T) {
	t.Parallel()
	t.Run("empty", func(t *testing.T) {
		if got := LargestAllowed(nil); got != (PhotoSize{}) {
			t.Fatalf("LargestAllowed(nil) = %+v", got)
		}
	})
	t.Run("largest pixel area", func(t *testing.T) {
		sizes := []PhotoSize{{Label: "Wide", Width: 200, Height: 100}, {Label: "Tall", Width: 100, Height: 300}, {Label: "Tiny", Width: 1, Height: 1}}
		if got := LargestAllowed(sizes); got.Label != "Tall" {
			t.Fatalf("LargestAllowed() = %+v, want Tall", got)
		}
	})
	t.Run("original preferred", func(t *testing.T) {
		sizes := []PhotoSize{{Label: "Original", Width: 50, Height: 50}, {Label: "Large", Width: 5000, Height: 5000}}
		if got := LargestAllowed(sizes); got.Label != "Original" {
			t.Fatalf("LargestAllowed() = %+v, want Original", got)
		}
	})
}

func TestPhotoMethodRequestParams(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	creds := Credentials{APIKey: "k"}
	checks := map[string]struct {
		call func(*Client) error
		want map[string]string
	}{
		"photos.getSizes":       {call: func(c *Client) error { _, err := c.PhotosGetSizes(ctx, creds, "p"); return err }, want: map[string]string{"method": "flickr.photos.getSizes", "photo_id": "p"}},
		"photos.getInfo":        {call: func(c *Client) error { _, err := c.PhotosGetInfo(ctx, creds, "p", "s"); return err }, want: map[string]string{"method": "flickr.photos.getInfo", "photo_id": "p", "secret": "s"}},
		"photos.getExif":        {call: func(c *Client) error { _, err := c.PhotosGetExif(ctx, creds, "p", "s"); return err }, want: map[string]string{"method": "flickr.photos.getExif", "photo_id": "p", "secret": "s"}},
		"photos.getAllContexts": {call: func(c *Client) error { _, err := c.PhotosGetAllContexts(ctx, creds, "p"); return err }, want: map[string]string{"method": "flickr.photos.getAllContexts", "photo_id": "p"}},
		"photos.comments.getList": {call: func(c *Client) error {
			_, err := c.PhotosCommentsGetList(ctx, creds, "p", ListOptions{Page: 2, PerPage: 50})
			return err
		}, want: map[string]string{"method": "flickr.photos.comments.getList", "photo_id": "p", "page": "2", "per_page": "50"}},
		"photos.getFavorites": {call: func(c *Client) error {
			_, err := c.PhotosGetFavorites(ctx, creds, "p", ListOptions{Page: 3, PerPage: 25})
			return err
		}, want: map[string]string{"method": "flickr.photos.getFavorites", "photo_id": "p", "page": "3", "per_page": "25"}},
		"photos.people.getList": {call: func(c *Client) error { _, err := c.PhotosPeopleGetList(ctx, creds, "p"); return err }, want: map[string]string{"method": "flickr.photos.people.getList", "photo_id": "p"}},
	}
	for name, check := range checks {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client, last := fixtureClient(t)
			if err := check.call(client); err != nil {
				t.Fatal(err)
			}
			for key, value := range check.want {
				if (*last)[key] != value {
					t.Errorf("%s = %q, want %q", key, (*last)[key], value)
				}
			}
		})
	}
}

func TestListOptionsParams(t *testing.T) {
	t.Parallel()
	client, last := fixtureClient(t)
	when := time.Unix(1700000000, 0)
	_, err := client.FavoritesGetList(context.Background(), Credentials{APIKey: "k"}, "n", ListOptions{
		PerPage: 900, Page: 2, MinUploadDate: when, MinFaveDate: when.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"extras": DefaultExtras, "per_page": "500", "page": "2", "min_upload_date": "1700000000", "min_fave_date": "1700003600", "user_id": "n"}
	for key, value := range want {
		if (*last)[key] != value {
			t.Errorf("param %s = %q, want %q", key, (*last)[key], value)
		}
	}
	if _, err := client.ContactsGetList(context.Background(), Credentials{APIKey: "k"}, ListOptions{Extras: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := (*last)["extras"]; ok {
		t.Error("contacts list must not send extras")
	}
	if _, err := client.PeopleGetPhotos(context.Background(), Credentials{APIKey: "k"}, "n", ListOptions{Extras: " url_m "}); err != nil {
		t.Fatal(err)
	}
	if (*last)["extras"] != "url_m" {
		t.Errorf("extras = %q", (*last)["extras"])
	}
}

func TestMalformedResponses(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"missing root": `{"stat":"ok"}`,
		"bad root":     `{"photos":"x","stat":"ok"}`,
		"bad items":    `{"photos":{"page":1,"photo":"x"},"stat":"ok"}`,
		"bad meta":     `{"photos":{"page":"abc"},"stat":"ok"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			_, err := NewClient(server.URL, server.Client()).PeopleGetPhotos(context.Background(), Credentials{APIKey: "k"}, "n", ListOptions{})
			if !errors.Is(err, ErrMalformedResponse) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"stat":"ok","user":[]}`)) }))
	defer server.Close()
	c := NewClient(server.URL, server.Client())
	if _, err := c.TestLogin(context.Background(), Credentials{APIKey: "k"}); !errors.Is(err, ErrMalformedResponse) {
		t.Fatalf("login error = %v", err)
	}
	if _, err := c.TestEcho(context.Background(), Credentials{APIKey: "k"}, nil); err != nil {
		t.Fatalf("echo error = %v", err)
	}
	if err := decodeObject([]byte(`[1]`), &map[string]Content{}); !errors.Is(err, ErrMalformedResponse) {
		t.Fatalf("decodeObject error = %v", err)
	}
	if _, err := c.PeopleGetInfo(context.Background(), Credentials{}, "n"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("missing key error = %v", err)
	}
}

func TestFlexibleDecoding(t *testing.T) {
	t.Parallel()
	var v struct {
		A, B, C, D, E Int
		S1, S2        String
		C1, C2, C3    Content
	}
	input := `{"A":5,"B":"6","C":"","D":null,"E":7.0,"S1":"x","S2":12,"C1":{"_content":"y"},"C2":"z","C3":{"_content":3}}`
	if err := jsonUnmarshal(input, &v); err != nil {
		t.Fatal(err)
	}
	if v.A != 5 || v.B != 6 || v.C != 0 || v.D != 0 || v.E != 7 || v.S1 != "x" || v.S2 != "12" || v.C1 != "y" || v.C2 != "z" || v.C3 != "3" {
		t.Fatalf("decoded = %+v", v)
	}
	if err := jsonUnmarshal(`{"A":"abc"}`, &v); err == nil {
		t.Fatal("expected error for non numeric Int")
	}
	if err := jsonUnmarshal(`{"S1":{}}`, &v); err != nil {
		t.Fatalf("object String should be kept as raw text: %v", err)
	}
	if err := jsonUnmarshal(`{"C1":{"_content":{"a":1}}}`, &v); err != nil {
		t.Fatalf("object content kept raw: %v", err)
	}
	if err := jsonUnmarshal(`{"C1":"unterminated`, &v); err == nil {
		t.Fatal("expected syntax error")
	}
}

func TestPaginatorFetchesLazily(t *testing.T) {
	t.Parallel()
	var fetched []int
	p := NewPaginator(0, func(_ context.Context, page int) (Page[int], error) {
		fetched = append(fetched, page)
		return Page[int]{Items: []int{page}, Page: page, Pages: 3}, nil
	})
	if len(fetched) != 0 {
		t.Fatal("constructor must not fetch")
	}
	var seen []int
	for p.Next(context.Background()) {
		seen = append(seen, p.Page().Items[0])
		if len(fetched) != len(seen) {
			t.Fatalf("fetched %v ahead of consumed %v", fetched, seen)
		}
	}
	if p.Err() != nil || len(seen) != 3 || seen[2] != 3 {
		t.Fatalf("seen = %v, err = %v", seen, p.Err())
	}
	if p.Next(context.Background()) || len(fetched) != 3 {
		t.Fatalf("fetched after end: %v", fetched)
	}
}

func TestPaginatorStopsOnEmptyPageAndError(t *testing.T) {
	t.Parallel()
	empty := NewPaginator(1, func(_ context.Context, page int) (Page[int], error) { return Page[int]{Page: page, Pages: 5}, nil })
	if !empty.Next(context.Background()) || empty.Next(context.Background()) {
		t.Fatal("empty page must end iteration after being returned")
	}
	boom := errors.New("boom")
	failing := NewPaginator(2, func(_ context.Context, page int) (Page[int], error) {
		if page == 2 {
			return Page[int]{Items: []int{1}, Page: 2, Pages: 9}, nil
		}
		return Page[int]{}, boom
	})
	if !failing.Next(context.Background()) || failing.Next(context.Background()) || !errors.Is(failing.Err(), boom) {
		t.Fatalf("err = %v", failing.Err())
	}
	if failing.Next(context.Background()) {
		t.Fatal("Next after error must stay false")
	}
	noPage := NewPaginator(1, func(_ context.Context, _ int) (Page[int], error) { return Page[int]{Items: []int{1}, Pages: 2}, nil })
	n := 0
	for noPage.Next(context.Background()) {
		n++
	}
	if n != 2 {
		t.Fatalf("pages without Page field = %d, want 2", n)
	}
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
