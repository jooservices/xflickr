package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jooservices/xflickr/internal/catalog/domain"
)

type captionRepositorySpy struct {
	enrichment domain.PhotoEnrichment
	calls      int
	err        error
}

func (f *captionRepositorySpy) UpsertEnrichment(_ context.Context, enrichment domain.PhotoEnrichment) error {
	f.calls++
	f.enrichment = enrichment
	return f.err
}

func TestCaptionServiceMapsEnrichment(t *testing.T) {
	payload := []byte(`{"text":"A red boat","objects":["boat"],"model":"vision","version":"revision-7"}`)
	repository := &captionRepositorySpy{}
	service, err := NewCaptionService(repository)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SaveCaption(t.Context(), 42, "vision", "revision-7", payload); err != nil {
		t.Fatalf("SaveCaption() error = %v", err)
	}
	want := domain.PhotoEnrichment{
		PhotoID: 42, Kind: domain.EnrichmentCaption, Model: "vision", ModelVersion: "revision-7", Payload: payload,
	}
	if repository.calls != 1 || !reflect.DeepEqual(repository.enrichment, want) {
		t.Fatalf("UpsertEnrichment() calls=%d enrichment=%+v, want %+v", repository.calls, repository.enrichment, want)
	}
}

func TestCaptionServiceRequiresRepositoryAndPropagatesError(t *testing.T) {
	if _, err := NewCaptionService(nil); err == nil {
		t.Fatal("NewCaptionService() accepted a nil repository")
	}
	wantErr := errors.New("catalog database failed")
	repository := &captionRepositorySpy{err: wantErr}
	service, err := NewCaptionService(repository)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SaveCaption(t.Context(), 42, "vision", "revision-7", []byte(`{}`)); !errors.Is(err, wantErr) {
		t.Fatalf("SaveCaption() error = %v, want database error", err)
	}
	if err := (*CaptionService)(nil).SaveCaption(t.Context(), 42, "vision", "revision-7", []byte(`{}`)); err == nil {
		t.Fatal("nil CaptionService accepted a write")
	}
}
