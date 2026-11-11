package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jooservices/xflickr/internal/catalog/domain"
)

type enrichmentReaderFake struct {
	enrichment domain.PhotoEnrichment
	found      bool
	err        error
	calls      int
	photoID    int64
	kind       domain.EnrichmentKind
	model      string
}

func (f *enrichmentReaderFake) GetEnrichment(_ context.Context, photoID int64, kind domain.EnrichmentKind, model string) (domain.PhotoEnrichment, bool, error) {
	f.calls++
	f.photoID, f.kind, f.model = photoID, kind, model
	return f.enrichment, f.found, f.err
}

func TestNeedsEnrichment(t *testing.T) {
	ctx := t.Context()
	payload := []byte(`{"caption":"Existing caption"}`)

	for _, kind := range []string{"caption", "embedding_meta"} {
		for _, test := range []struct {
			name          string
			found         bool
			stored        string
			targetVersion string
			want          bool
		}{
			{name: "same version", found: true, stored: "v1", want: false},
			{name: "version comparison is case sensitive", found: true, stored: "v1", targetVersion: "V1", want: true},
			{name: "different version", found: true, stored: "v0", want: true},
			{name: "different model only", found: false, stored: "v1", want: true},
			{name: "no enrichment for kind", found: false, want: true},
		} {
			t.Run(kind+"/"+test.name, func(t *testing.T) {
				reader := &enrichmentReaderFake{
					enrichment: domain.PhotoEnrichment{
						PhotoID: 17, Kind: domain.EnrichmentKind(kind), Model: "model-A",
						ModelVersion: test.stored, Payload: append([]byte(nil), payload...),
					},
					found: test.found,
				}
				service := EnrichmentFreshness{Reader: reader}
				targetVersion := test.targetVersion
				if targetVersion == "" {
					targetVersion = "v1"
				}

				got, err := service.NeedsEnrichment(ctx, 17, kind, "model-A", targetVersion)
				if err != nil || got != test.want {
					t.Fatalf("NeedsEnrichment() = %v, %v; want %v, nil", got, err, test.want)
				}
				if reader.calls != 1 || reader.photoID != 17 || reader.kind != domain.EnrichmentKind(kind) || reader.model != "model-A" {
					t.Fatalf("reader call = %+v", reader)
				}
				if !bytes.Equal(reader.enrichment.Payload, payload) {
					t.Fatalf("payload changed: %s", reader.enrichment.Payload)
				}
			})
		}
	}
}

func TestNeedsEnrichmentRejectsInvalidInput(t *testing.T) {
	valid := func(reader EnrichmentReader) EnrichmentFreshness { return EnrichmentFreshness{Reader: reader} }
	for _, test := range []struct {
		name    string
		ctx     context.Context
		photo   int64
		kind    string
		model   string
		version string
		reader  bool
	}{
		{name: "nil context", ctx: nil, photo: 1, kind: "caption", model: "m", version: "v", reader: true},
		{name: "zero photo id", ctx: context.Background(), photo: 0, kind: "caption", model: "m", version: "v", reader: true},
		{name: "negative photo id", ctx: context.Background(), photo: -1, kind: "caption", model: "m", version: "v", reader: true},
		{name: "unsupported kind", ctx: context.Background(), photo: 1, kind: "Caption", model: "m", version: "v", reader: true},
		{name: "empty model", ctx: context.Background(), photo: 1, kind: "caption", model: "", version: "v", reader: true},
		{name: "blank model", ctx: context.Background(), photo: 1, kind: "caption", model: " \t ", version: "v", reader: true},
		{name: "empty version", ctx: context.Background(), photo: 1, kind: "caption", model: "m", version: "", reader: true},
		{name: "blank version", ctx: context.Background(), photo: 1, kind: "caption", model: "m", version: " \n ", reader: true},
		{name: "nil reader", ctx: context.Background(), photo: 1, kind: "caption", model: "m", version: "v"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &enrichmentReaderFake{}
			var service EnrichmentFreshness
			if test.reader {
				service = valid(reader)
			}
			if _, err := service.NeedsEnrichment(test.ctx, test.photo, test.kind, test.model, test.version); err == nil {
				t.Fatal("NeedsEnrichment() accepted invalid input")
			}
			if reader.calls != 0 {
				t.Fatalf("invalid input reached reader %d times", reader.calls)
			}
		})
	}
}

func TestNeedsEnrichmentPropagatesReadError(t *testing.T) {
	readErr := errors.New("database unavailable")
	reader := &enrichmentReaderFake{err: readErr}
	service := EnrichmentFreshness{Reader: reader}

	_, err := service.NeedsEnrichment(t.Context(), 9, "caption", "model", "version")
	if !errors.Is(err, readErr) {
		t.Fatalf("NeedsEnrichment() error = %v; want wrapped read error", err)
	}
	if !strings.HasPrefix(err.Error(), "catalog: check enrichment freshness: ") {
		t.Fatalf("NeedsEnrichment() error = %q; want catalog context", err)
	}
}
