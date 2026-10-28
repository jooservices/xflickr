package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
	"github.com/jooservices/xflickr/pkg/flickr"
)

func TestParsePhotoFacetJob(t *testing.T) {
	job, err := ParsePhotoFacetJob([]byte(`{"photo_id":42,"key_id":7,"facet":"info","class":"photo"}`))
	if err != nil || job.PhotoID != 42 || job.KeyID != 7 || job.Facet != PhotoInfoFacet || job.Class != quotadomain.Photo {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	for _, raw := range []string{
		`{`,
		`{"photo_id":0,"key_id":7,"facet":"info"}`,
		`{"photo_id":42,"key_id":0,"facet":"info"}`,
		`{"photo_id":42,"key_id":7,"facet":"unknown"}`,
		`{"photo_id":42,"key_id":7,"facet":"info","class":"admin"}`,
	} {
		if _, err := ParsePhotoFacetJob([]byte(raw)); err == nil {
			t.Errorf("ParsePhotoFacetJob(%s) succeeded", raw)
		}
	}
}

func TestPhotoFacetsFetchesEveryFacetAfterSizesAndStripsSecrets(t *testing.T) {
	checked := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	store := &photoFacetStoreFake{source: catalog.PhotoFacetSource{Secret: "photo-secret", LastUpdate: checked, SizesStatus: catalog.SizesConfirmed, SizesCheckedAt: &checked}, exists: true}
	client := &photoFacetClientFake{
		info:     flickr.PhotoInfo{ID: "42", Secret: "private-secret", OriginalSecret: "original-secret"},
		exif:     flickr.PhotoExif{ID: "42", Secret: "private-secret", Camera: "Fake Camera"},
		contexts: flickr.PhotoContexts{Sets: []flickr.PhotoContext{{ID: "set-1"}}},
		comments: map[int]flickr.Page[flickr.PhotoComment]{
			1: {Page: 1, Pages: 2, Items: []flickr.PhotoComment{{ID: "c1"}}},
			2: {Page: 2, Pages: 2, Items: []flickr.PhotoComment{{ID: "c2"}}},
		},
		favorites: map[int]flickr.Page[flickr.PhotoFavorite]{
			1: {Page: 1, Pages: 2, Items: []flickr.PhotoFavorite{{NSID: "u1"}}},
			2: {Page: 2, Pages: 2, Items: []flickr.PhotoFavorite{{NSID: "u2"}}},
		},
	}
	handler := PhotoFacets{Store: store, Flickr: client, Now: func() time.Time { return checked.Add(time.Minute) }}
	for _, facet := range []string{PhotoInfoFacet, PhotoExifFacet, PhotoContextsFacet, PhotoCommentsFacet, PhotoFavoritesFacet} {
		if err := handler.Handle(context.Background(), PhotoFacetJob{PhotoID: 42, KeyID: 7, Facet: facet, Class: quotadomain.Photo}); err != nil {
			t.Fatalf("Handle(%s): %v", facet, err)
		}
		if got := store.lastFacet; got != facet {
			t.Fatalf("stored facet = %q, want %q", got, facet)
		}
		if !store.lastFetchedAt.Equal(checked.Add(time.Minute)) || !store.lastPhotoUpdatedAt.Equal(checked) {
			t.Fatalf("stored timestamps = %s / %s", store.lastPhotoUpdatedAt, store.lastFetchedAt)
		}
	}
	if client.infoSecret != "photo-secret" || client.exifSecret != "photo-secret" {
		t.Fatalf("photo secret was not passed to the API: info=%q exif=%q", client.infoSecret, client.exifSecret)
	}
	if client.commentCalls != 2 || client.favoriteCalls != 2 {
		t.Fatalf("page calls comments=%d favorites=%d, want 2 each", client.commentCalls, client.favoriteCalls)
	}
	if strings.Contains(string(store.data[PhotoInfoFacet]), "private-secret") || strings.Contains(string(store.data[PhotoInfoFacet]), "original-secret") {
		t.Fatalf("photo info persisted an API secret: %s", store.data[PhotoInfoFacet])
	}
	if strings.Contains(string(store.data[PhotoExifFacet]), "private-secret") {
		t.Fatalf("photo EXIF persisted an API secret: %s", store.data[PhotoExifFacet])
	}
	var comments []flickr.PhotoComment
	if err := json.Unmarshal(store.data[PhotoCommentsFacet], &comments); err != nil || len(comments) != 2 {
		t.Fatalf("comments=%+v err=%v", comments, err)
	}
	var favorites []flickr.PhotoFavorite
	if err := json.Unmarshal(store.data[PhotoFavoritesFacet], &favorites); err != nil || len(favorites) != 2 {
		t.Fatalf("favorites=%+v err=%v", favorites, err)
	}
}

func TestPhotoFacetsRequiresCurrentSizeConfirmation(t *testing.T) {
	checked := time.Now().UTC()
	for _, source := range []catalog.PhotoFacetSource{
		{LastUpdate: checked, SizesStatus: catalog.SizesNone},
		{LastUpdate: checked, SizesStatus: catalog.SizesConfirmed},
		{LastUpdate: checked, SizesStatus: catalog.SizesConfirmed, SizesCheckedAt: timePointer(checked.Add(-time.Second))},
	} {
		store := &photoFacetStoreFake{source: source, exists: true}
		client := &photoFacetClientFake{}
		if err := (PhotoFacets{Store: store, Flickr: client}).Handle(context.Background(), PhotoFacetJob{PhotoID: 42, KeyID: 7, Facet: PhotoInfoFacet}); err != nil {
			t.Fatalf("Handle with stale size state: %v", err)
		}
		if client.infoCalls != 0 || len(store.data) != 0 {
			t.Fatalf("stale size state called Flickr or stored data: calls=%d data=%v", client.infoCalls, store.data)
		}
	}
}

func TestPhotoFacetsTerminalErrorsStoreEmptyAndTransientErrorsRetry(t *testing.T) {
	checked := time.Now().UTC()
	source := catalog.PhotoFacetSource{LastUpdate: checked, SizesStatus: catalog.SizesConfirmed, SizesCheckedAt: &checked}
	for facet, want := range map[string]string{
		PhotoInfoFacet:      `{}`,
		PhotoExifFacet:      `{}`,
		PhotoContextsFacet:  `{"set":[],"pool":[]}`,
		PhotoCommentsFacet:  `[]`,
		PhotoFavoritesFacet: `[]`,
	} {
		store := &photoFacetStoreFake{source: source, exists: true}
		client := &photoFacetClientFake{infoErr: flickr.ErrNotFound, exifErr: flickr.ErrPermission, contextsErr: flickr.ErrNotFound, commentsErr: flickr.ErrPermission, favoritesErr: flickr.ErrNotFound}
		if err := (PhotoFacets{Store: store, Flickr: client}).Handle(context.Background(), PhotoFacetJob{PhotoID: 42, KeyID: 7, Facet: facet}); err != nil {
			t.Fatalf("terminal Handle(%s): %v", facet, err)
		}
		if got := string(store.data[facet]); got != want {
			t.Errorf("stored %s = %s, want %s", facet, got, want)
		}
	}
	store := &photoFacetStoreFake{source: source, exists: true}
	client := &photoFacetClientFake{infoErr: errors.New("temporary transport failure")}
	err := (PhotoFacets{Store: store, Flickr: client}).Handle(context.Background(), PhotoFacetJob{PhotoID: 42, KeyID: 7, Facet: PhotoInfoFacet})
	if err == nil || len(store.data) != 0 {
		t.Fatalf("transient error=%v data=%v; want retryable error and no stored result", err, store.data)
	}
}

func TestPhotoFacetsPropagatesStoreAndPaginationSafetyErrors(t *testing.T) {
	checked := time.Now().UTC()
	store := &photoFacetStoreFake{source: catalog.PhotoFacetSource{LastUpdate: checked, SizesStatus: catalog.SizesConfirmed, SizesCheckedAt: &checked}, exists: true, setErr: errors.New("database unavailable")}
	client := &photoFacetClientFake{info: flickr.PhotoInfo{ID: "42"}}
	if err := (PhotoFacets{Store: store, Flickr: client}).Handle(context.Background(), PhotoFacetJob{PhotoID: 42, KeyID: 7, Facet: PhotoInfoFacet}); err == nil {
		t.Fatal("store error was swallowed")
	}

	store = &photoFacetStoreFake{source: catalog.PhotoFacetSource{LastUpdate: checked, SizesStatus: catalog.SizesConfirmed, SizesCheckedAt: &checked}, exists: true}
	commentPages := make(map[int]flickr.Page[flickr.PhotoComment], maxPhotoFacetPages)
	for page := 1; page <= maxPhotoFacetPages; page++ {
		commentPages[page] = flickr.Page[flickr.PhotoComment]{Page: page, Pages: maxPhotoFacetPages + 1, Items: []flickr.PhotoComment{{ID: "c1"}}}
	}
	client = &photoFacetClientFake{comments: commentPages}
	err := (PhotoFacets{Store: store, Flickr: client}).Handle(context.Background(), PhotoFacetJob{PhotoID: 42, KeyID: 7, Facet: PhotoCommentsFacet})
	if err == nil || !strings.Contains(err.Error(), "safety limit") || len(store.data) != 0 {
		t.Fatalf("pagination error=%v data=%v; want bounded retryable error", err, store.data)
	}
}

type photoFacetStoreFake struct {
	source             catalog.PhotoFacetSource
	exists             bool
	setErr             error
	lastFacet          string
	lastPhotoUpdatedAt time.Time
	lastFetchedAt      time.Time
	data               map[string][]byte
}

func (s *photoFacetStoreFake) LoadPhotoFacetSource(context.Context, int64) (catalog.PhotoFacetSource, bool, error) {
	return s.source, s.exists, nil
}

func (s *photoFacetStoreFake) SetPhotoFacet(_ context.Context, _ int64, facet string, data []byte, photoUpdatedAt, fetchedAt time.Time) error {
	if s.setErr != nil {
		return s.setErr
	}
	if s.data == nil {
		s.data = make(map[string][]byte)
	}
	s.lastFacet, s.lastPhotoUpdatedAt, s.lastFetchedAt = facet, photoUpdatedAt, fetchedAt
	s.data[facet] = append([]byte(nil), data...)
	return nil
}

type photoFacetClientFake struct {
	info          flickr.PhotoInfo
	exif          flickr.PhotoExif
	contexts      flickr.PhotoContexts
	comments      map[int]flickr.Page[flickr.PhotoComment]
	favorites     map[int]flickr.Page[flickr.PhotoFavorite]
	infoErr       error
	exifErr       error
	contextsErr   error
	commentsErr   error
	favoritesErr  error
	infoCalls     int
	exifCalls     int
	infoSecret    string
	exifSecret    string
	commentCalls  int
	favoriteCalls int
}

func (f *photoFacetClientFake) PhotosGetInfo(_ context.Context, _ int64, _ string, secret string) (flickr.PhotoInfo, error) {
	f.infoCalls++
	f.infoSecret = secret
	return f.info, f.infoErr
}

func (f *photoFacetClientFake) PhotosGetExif(_ context.Context, _ int64, _ string, secret string) (flickr.PhotoExif, error) {
	f.exifCalls++
	f.exifSecret = secret
	return f.exif, f.exifErr
}

func (f *photoFacetClientFake) PhotosGetAllContexts(context.Context, int64, string) (flickr.PhotoContexts, error) {
	return f.contexts, f.contextsErr
}

func (f *photoFacetClientFake) PhotosCommentsGetList(_ context.Context, _ int64, _ string, page int) (flickr.Page[flickr.PhotoComment], error) {
	f.commentCalls++
	return f.comments[page], f.commentsErr
}

func (f *photoFacetClientFake) PhotosGetFavorites(_ context.Context, _ int64, _ string, page int) (flickr.Page[flickr.PhotoFavorite], error) {
	f.favoriteCalls++
	return f.favorites[page], f.favoritesErr
}

func timePointer(value time.Time) *time.Time { return &value }
