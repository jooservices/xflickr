package elasticsearch

import (
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/jooservices/xflickr/internal/search/app"
)

func TestCreateReportsEncodingAndRequestConstructionFailures(t *testing.T) {
	wantErr := errors.New("encode failed")
	t.Run("encoding", func(t *testing.T) {
		index := &Index{prefix: "wt_jooxf522-"}
		err := index.create(t.Context(), func(any) ([]byte, error) { return nil, wantErr })
		if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "encode index mapping") {
			t.Fatalf("create() error = %v, want wrapped encoding error", err)
		}
	})
	t.Run("request construction", func(t *testing.T) {
		index := &Index{baseURL: "http://[::1", prefix: "wt_jooxf522-", client: http.DefaultClient}
		err := index.Create(t.Context())
		var requestErr requestError
		if !errors.As(err, &requestErr) || requestErr.operation != "create index" {
			t.Fatalf("Create() error = %v, want request construction error", err)
		}
	})
}

func TestUpsertReportsSerializationAndRequestConstructionFailures(t *testing.T) {
	t.Run("serialization", func(t *testing.T) {
		embedding := make([]float32, 1024)
		embedding[0] = float32(math.NaN())
		index := &Index{baseURL: "http://search.example", prefix: "wt_jooxf522-", client: http.DefaultClient}
		err := index.Upsert(t.Context(), app.Document{ID: 42, Embedding: embedding})
		if err == nil || !strings.Contains(err.Error(), "encode document") {
			t.Fatalf("Upsert() error = %v, want serialization error", err)
		}
	})
	t.Run("request construction", func(t *testing.T) {
		index := &Index{baseURL: "http://[::1", prefix: "wt_jooxf522-", client: http.DefaultClient}
		err := index.Upsert(t.Context(), app.Document{ID: 42})
		var requestErr requestError
		if !errors.As(err, &requestErr) || requestErr.operation != "upsert document" {
			t.Fatalf("Upsert() error = %v, want request construction error", err)
		}
	})
}
