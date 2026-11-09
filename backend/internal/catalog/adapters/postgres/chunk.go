package postgres

import "fmt"

// forEachChunk invokes fn for successive slices of at most size items.
// An empty input is a no-op. fn errors stop further chunks and are returned.
func forEachChunk[T any](items []T, size int, fn func([]T) error) error {
	if size <= 0 {
		return fmt.Errorf("catalog: chunk size must be positive, got %d", size)
	}
	for start := 0; start < len(items); start += size {
		end := start + size
		if end > len(items) {
			end = len(items)
		}
		if err := fn(items[start:end]); err != nil {
			return err
		}
	}
	return nil
}
