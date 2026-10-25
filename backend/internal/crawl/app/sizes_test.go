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

type sizesStoreFake struct {
	photo   catalog.Photo
	exists  bool
	loadErr error
	setErr  error
	sets    int
	data    []byte
	status  catalog.SizesStatus
	checked time.Time
}

func (s *sizesStoreFake) LoadSizes(context.Context, int64) (catalog.Photo, bool, error) {
	return s.photo, s.exists, s.loadErr
}

func (s *sizesStoreFake) SetSizes(_ context.Context, _ int64, data []byte, status catalog.SizesStatus, checked time.Time) error {
	s.sets++
	s.data, s.status, s.checked = data, status, checked
	return s.setErr
}

type sizesClientFake struct {
	sizes   []flickr.PhotoSize
	err     error
	calls   int
	keyID   int64
	photoID string
	class   quotadomain.Class
}

func (f *sizesClientFake) GetSizes(_ context.Context, keyID int64, photoID string) ([]flickr.PhotoSize, error) {
	f.calls++
	f.keyID, f.photoID = keyID, photoID
	return f.sizes, f.err
}

func (f *sizesClientFake) GetSizesClass(ctx context.Context, keyID int64, photoID string, class quotadomain.Class) ([]flickr.PhotoSize, error) {
	f.class = class
	return f.GetSizes(ctx, keyID, photoID)
}

func sizesRig() (Sizes, *sizesStoreFake, *sizesClientFake) {
	checked := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	store := &sizesStoreFake{exists: true, photo: catalog.Photo{
		ID: 40, LastUpdate: checked.Add(-time.Hour), SizesStatus: catalog.SizesProvisional,
	}}
	client := &sizesClientFake{sizes: []flickr.PhotoSize{{Label: "Original", Width: 100, Height: 80, Source: "https://example.test/original"}}}
	handler := Sizes{Store: store, Flickr: client, Now: func() time.Time { return checked }}
	return handler, store, client
}

func TestParseSizesJob(t *testing.T) {
	job, err := ParseSizesJob([]byte(`{"photo_id":40,"key_id":12}`))
	if err != nil || job.PhotoID != 40 || job.KeyID != 12 {
		t.Fatalf("ParseSizesJob() = %+v, %v", job, err)
	}
	for _, raw := range []string{`{`, `{}`, `{"photo_id":0,"key_id":1}`, `{"photo_id":1,"key_id":0}`} {
		if _, err := ParseSizesJob([]byte(raw)); err == nil {
			t.Errorf("ParseSizesJob(%s) succeeded", raw)
		}
	}
	job, err = ParseSizesJob([]byte(`{"photo_id":40,"key_id":12,"class":"interactive"}`))
	if err != nil || job.Class != "interactive" {
		t.Fatalf("ParseSizesJob interactive = %+v, %v", job, err)
	}
	if _, err := ParseSizesJob([]byte(`{"photo_id":40,"key_id":12,"class":"unknown"}`)); err == nil {
		t.Fatal("accepted unsupported quota class")
	}
}

func TestSizesUsesDirectActionQuotaClass(t *testing.T) {
	handler, _, client := sizesRig()
	if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12, Class: string(quotadomain.Interactive)}); err != nil {
		t.Fatal(err)
	}
	if client.class != quotadomain.Interactive {
		t.Fatalf("class = %q", client.class)
	}
}

func TestSizesConfirmsFetchedValues(t *testing.T) {
	handler, store, client := sizesRig()
	if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); err != nil {
		t.Fatal(err)
	}
	if client.calls != 1 || client.keyID != 12 || client.photoID != "40" {
		t.Fatalf("Flickr calls = %+v", client)
	}
	var got []flickr.PhotoSize
	if err := json.Unmarshal(store.data, &got); err != nil || len(got) != 1 || got[0].Label != "Original" {
		t.Fatalf("stored sizes = %s, %v", store.data, err)
	}
	if store.status != catalog.SizesConfirmed || !store.checked.Equal(handler.Now()) {
		t.Fatalf("stored outcome = %s at %s", store.status, store.checked)
	}
}

func TestSizesSkipRules(t *testing.T) {
	handler, store, client := sizesRig()
	tests := []struct {
		name  string
		setup func(*sizesStoreFake)
	}{
		{name: "photo missing", setup: func(s *sizesStoreFake) { s.exists = false }},
		{name: "already unavailable", setup: func(s *sizesStoreFake) { s.photo.SizesStatus = catalog.SizesUnavailable }},
		{name: "confirmed unchanged", setup: func(s *sizesStoreFake) {
			checked := s.photo.LastUpdate
			s.photo.SizesStatus, s.photo.SizesCheckedAt = catalog.SizesConfirmed, &checked
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, store, client := sizesRig()
			tt.setup(store)
			if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); err != nil {
				t.Fatal(err)
			}
			if client.calls != 0 || store.sets != 0 {
				t.Fatalf("skip made calls: Flickr=%d writes=%d", client.calls, store.sets)
			}
		})
	}
	// A confirmed result from an older photo version is refreshed.
	checked := store.photo.LastUpdate.Add(-time.Second)
	store.photo.SizesStatus, store.photo.SizesCheckedAt = catalog.SizesConfirmed, &checked
	if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); err != nil || client.calls != 1 {
		t.Fatalf("stale confirmed result: calls=%d err=%v", client.calls, err)
	}
}

func TestSizesMarksUnavailableForTerminalFlickrErrors(t *testing.T) {
	for _, cause := range []error{flickr.ErrNotFound, flickr.ErrPermission} {
		t.Run(cause.Error(), func(t *testing.T) {
			handler, store, client := sizesRig()
			client.err = &flickr.CallError{Kind: cause}
			if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); err != nil {
				t.Fatal(err)
			}
			if store.status != catalog.SizesUnavailable || string(store.data) != "[]" || client.calls != 1 {
				t.Fatalf("terminal outcome: status=%s data=%s calls=%d", store.status, store.data, client.calls)
			}
		})
	}
}

func TestSizesPropagatesOtherErrors(t *testing.T) {
	boom := errors.New("temporary failure")
	handler, store, client := sizesRig()
	store.loadErr = boom
	if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); !errors.Is(err, boom) {
		t.Fatalf("load error = %v", err)
	}
	store.loadErr = nil
	client.err = boom
	if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); !errors.Is(err, boom) || !strings.Contains(err.Error(), "get sizes") {
		t.Fatalf("Flickr error = %v", err)
	}
	client.err = nil
	store.setErr = boom
	if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); !errors.Is(err, boom) {
		t.Fatalf("store error = %v", err)
	}
}

func TestSizesRequiresDependenciesAndHonorsCanceledContext(t *testing.T) {
	if err := (Sizes{}).Handle(t.Context(), SizesJob{PhotoID: 1, KeyID: 1}); err == nil {
		t.Fatal("incomplete handler succeeded")
	}
	handler, store, client := sizesRig()
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := handler.Handle(canceled, SizesJob{PhotoID: 40, KeyID: 12}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled handler error = %v", err)
	}
	if client.calls != 0 || store.sets != 0 {
		t.Fatalf("canceled handler had effects: calls=%d writes=%d", client.calls, store.sets)
	}
	if err := handler.Handle(t.Context(), SizesJob{}); err == nil {
		t.Fatal("invalid job succeeded")
	}
}

func TestSizesUnavailableIsNotRetriedOnRepeatedJobs(t *testing.T) {
	handler, store, client := sizesRig()
	client.err = &flickr.CallError{Kind: flickr.ErrNotFound}
	if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); err != nil {
		t.Fatal(err)
	}
	store.photo.SizesStatus = store.status
	for range 3 {
		if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); err != nil {
			t.Fatal(err)
		}
	}
	if client.calls != 1 || store.status != catalog.SizesUnavailable {
		t.Fatalf("unavailable photo re-requested: calls=%d status=%s", client.calls, store.status)
	}
}

func TestSizesQuotaExceededWaitsInsteadOfFailing(t *testing.T) {
	handler, store, client := sizesRig()
	client.err = flickr.ErrQuotaExceeded
	err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12})
	var waiting *QuotaWaitError
	if !errors.As(err, &waiting) || waiting.Wait != DefaultTiming.QuotaWait || !errors.Is(err, flickr.ErrQuotaExceeded) {
		t.Fatalf("Handle() = %v", err)
	}
	if store.sets != 0 {
		t.Fatal("quota wait stored a terminal outcome")
	}
	client.err = NewQuotaWaitError(flickr.ErrQuotaExceeded, 7*time.Minute)
	if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); !errors.As(err, &waiting) || waiting.Wait != 7*time.Minute {
		t.Fatalf("Handle() kept wait = %v", err)
	}
	if got := waiting.Error(); !strings.Contains(got, "quota exceeded") {
		t.Fatalf("Error() = %q", got)
	}
}
