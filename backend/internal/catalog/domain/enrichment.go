package domain

import "time"

// EnrichmentKind identifies the AI output stored for a photo.
type EnrichmentKind string

const (
	EnrichmentCaption       EnrichmentKind = "caption"
	EnrichmentEmbeddingMeta EnrichmentKind = "embedding_meta"
)

// PhotoEnrichment stores a versioned AI result for a photo.
type PhotoEnrichment struct {
	PhotoID      int64
	Kind         EnrichmentKind
	Model        string
	ModelVersion string
	Payload      []byte
	CreatedAt    time.Time
}

// Asset stores metadata for a temporary photo file.
type Asset struct {
	ID        int64
	PhotoID   int64
	SizeLabel string
	SHA256    []byte
	Bytes     int64
	BlobKey   string
	ExpiresAt time.Time
	CreatedAt time.Time
}
