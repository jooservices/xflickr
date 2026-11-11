package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type captionerSpy struct {
	calls         int
	gotContext    context.Context
	gotImage      []byte
	caption       Caption
	err           error
	afterDescribe func()
	order         *[]string
}

func (f *captionerSpy) Describe(ctx context.Context, image []byte) (Caption, error) {
	f.calls++
	f.gotContext = ctx
	f.gotImage = image
	if f.order != nil {
		*f.order = append(*f.order, "describe")
	}
	if f.afterDescribe != nil {
		f.afterDescribe()
	}
	return f.caption, f.err
}

type captionStoreSpy struct {
	calls     int
	photoID   int64
	caption   Caption
	err       error
	afterSave func()
	order     *[]string
}

func (f *captionStoreSpy) SaveCaption(_ context.Context, photoID int64, caption Caption) error {
	f.calls++
	f.photoID = photoID
	f.caption = caption
	if f.order != nil {
		*f.order = append(*f.order, "save")
	}
	if f.afterSave != nil {
		f.afterSave()
	}
	return f.err
}

func TestNewEnricherRequiresDependencies(t *testing.T) {
	captioner := &captionerSpy{}
	store := &captionStoreSpy{}
	for _, test := range []struct {
		name      string
		captioner Captioner
		store     CaptionStore
	}{
		{name: "missing captioner", store: store},
		{name: "missing store", captioner: captioner},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewEnricher(test.captioner, test.store); !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("NewEnricher() error = %v, want ErrInvalidConfiguration", err)
			}
		})
	}
}

func TestCaptionPhotoRejectsUnconfiguredEnricherAndNilContext(t *testing.T) {
	if err := (*Enricher)(nil).CaptionPhoto(t.Context(), 42, []byte{1}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("nil Enricher error = %v", err)
	}
	if err := (&Enricher{}).CaptionPhoto(t.Context(), 42, []byte{1}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("unconfigured Enricher error = %v", err)
	}
	enricher, err := NewEnricher(&captionerSpy{}, &captionStoreSpy{})
	if err != nil {
		t.Fatal(err)
	}
	if err := enricher.CaptionPhoto(nil, 42, []byte{1}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("nil context error = %v", err)
	}
}

func TestCaptionPhotoSavesValidatedCaption(t *testing.T) {
	caption := Caption{Text: "A red boat on the water", Objects: []string{"boat", "water"}, Model: "vision", Version: "revision-7"}
	image := []byte{1, 2, 3, 4}
	order := make([]string, 0, 2)
	captioner := &captionerSpy{caption: caption, order: &order}
	store := &captionStoreSpy{order: &order}
	enricher, err := NewEnricher(captioner, store)
	if err != nil {
		t.Fatal(err)
	}

	if err := enricher.CaptionPhoto(t.Context(), 42, image); err != nil {
		t.Fatalf("CaptionPhoto() error = %v", err)
	}
	if captioner.calls != 1 || captioner.gotContext != t.Context() || !reflect.DeepEqual(captioner.gotImage, image) {
		t.Fatalf("Describe() calls=%d image=%v", captioner.calls, captioner.gotImage)
	}
	if store.calls != 1 || store.photoID != 42 || !reflect.DeepEqual(store.caption, caption) {
		t.Fatalf("SaveCaption() calls=%d photoID=%d caption=%+v", store.calls, store.photoID, store.caption)
	}
	if !reflect.DeepEqual(order, []string{"describe", "save"}) {
		t.Fatalf("call order = %v", order)
	}
}

func TestCaptionPhotoReturnsErrorsWithoutFalseSuccess(t *testing.T) {
	captionerErr := errors.New("caption provider failed")
	storeErr := errors.New("database failed")
	validCaption := Caption{Text: "A red boat", Model: "vision", Version: "revision-7"}
	for _, test := range []struct {
		name                string
		photoID             int64
		image               []byte
		caption             Caption
		captionErr          error
		storeErr            error
		cancelBeforeCall    bool
		cancelAfterDescribe bool
		cancelAfterSave     bool
		wantErr             error
		wantDescribeCalls   int
		wantStoreCalls      int
	}{
		{name: "invalid photo id", photoID: 0, image: []byte{1}, caption: validCaption, wantErr: ErrInvalidPhotoID, wantDescribeCalls: 0},
		{name: "empty image", photoID: 42, caption: validCaption, wantErr: ErrInvalidImage, wantDescribeCalls: 0},
		{name: "canceled context", photoID: 42, image: []byte{1}, caption: validCaption, cancelBeforeCall: true, wantErr: context.Canceled, wantDescribeCalls: 0},
		{name: "canceled during description", photoID: 42, image: []byte{1}, caption: validCaption, cancelAfterDescribe: true, wantErr: context.Canceled, wantDescribeCalls: 1},
		{name: "canceled during save", photoID: 42, image: []byte{1}, caption: validCaption, cancelAfterSave: true, wantErr: context.Canceled, wantDescribeCalls: 1, wantStoreCalls: 1},
		{name: "captioner error", photoID: 42, image: []byte{1}, captionErr: captionerErr, wantErr: captionerErr, wantDescribeCalls: 1},
		{name: "invalid caption", photoID: 42, image: []byte{1}, caption: Caption{Model: "vision", Version: "revision-7"}, wantErr: ErrInvalidCaption, wantDescribeCalls: 1},
		{name: "store error", photoID: 42, image: []byte{1}, caption: validCaption, storeErr: storeErr, wantErr: storeErr, wantDescribeCalls: 1, wantStoreCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			if test.cancelBeforeCall {
				cancel()
			}
			captioner := &captionerSpy{caption: test.caption, err: test.captionErr}
			if test.cancelAfterDescribe {
				captioner.afterDescribe = cancel
			}
			store := &captionStoreSpy{err: test.storeErr}
			if test.cancelAfterSave {
				store.afterSave = cancel
			}
			enricher, err := NewEnricher(captioner, store)
			if err != nil {
				t.Fatal(err)
			}
			err = enricher.CaptionPhoto(ctx, test.photoID, test.image)
			if test.wantErr == nil && err != nil {
				t.Fatalf("CaptionPhoto() error = %v", err)
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("CaptionPhoto() error = %v, want %v", err, test.wantErr)
			}
			if captioner.calls != test.wantDescribeCalls || store.calls != test.wantStoreCalls {
				t.Fatalf("Describe() calls=%d, SaveCaption() calls=%d", captioner.calls, store.calls)
			}
		})
	}
}
