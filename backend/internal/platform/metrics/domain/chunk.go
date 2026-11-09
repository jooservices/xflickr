package domain

// MaxWriteBatch is the largest number of samples written in one SQL statement.
const MaxWriteBatch = 500

// ChunkSamples splits samples into batches of at most size rows.
func ChunkSamples[T any](items []T, size int) [][]T {
	if size <= 0 {
		size = MaxWriteBatch
	}
	if len(items) == 0 {
		return nil
	}
	out := make([][]T, 0, (len(items)+size-1)/size)
	for start := 0; start < len(items); start += size {
		end := start + size
		if end > len(items) {
			end = len(items)
		}
		out = append(out, items[start:end])
	}
	return out
}
