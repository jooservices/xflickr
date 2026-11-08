package app

import "github.com/jooservices/xflickr/internal/platform/metrics/domain"

// MaxWriteBatch mirrors domain.MaxWriteBatch for callers in this package.
const MaxWriteBatch = domain.MaxWriteBatch

// ChunkSamples splits samples into batches of at most size rows.
func ChunkSamples[T any](items []T, size int) [][]T {
	return domain.ChunkSamples(items, size)
}
