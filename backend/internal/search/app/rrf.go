// Package app contains search application types and use cases.
package app

import (
	"errors"
	"fmt"
	"sort"
)

const rrfK = 60

// ErrInvalidRanking reports a branch containing an empty or repeated ID.
var ErrInvalidRanking = errors.New("search: invalid ranking")

// RankedHit is an ID and its Reciprocal Rank Fusion score and source ranks.
type RankedHit struct {
	ID         string
	Score      float64
	BM25Rank   int
	VectorRank int
}

// FuseRRF merges BM25 and vector rankings using Reciprocal Rank Fusion.
func FuseRRF(bm25, vector []string) ([]RankedHit, error) {
	if err := validateRanking(bm25); err != nil {
		return nil, err
	}
	if err := validateRanking(vector); err != nil {
		return nil, err
	}

	hits := make(map[string]RankedHit, len(bm25)+len(vector))
	for index, id := range bm25 {
		hit := hits[id]
		hit.ID = id
		hit.BM25Rank = index + 1
		hits[id] = hit
	}
	for index, id := range vector {
		hit := hits[id]
		hit.ID = id
		hit.VectorRank = index + 1
		hits[id] = hit
	}

	result := make([]RankedHit, 0, len(hits))
	for _, hit := range hits {
		if hit.BM25Rank > 0 {
			hit.Score += 1 / float64(rrfK+hit.BM25Rank)
		}
		if hit.VectorRank > 0 {
			hit.Score += 1 / float64(rrfK+hit.VectorRank)
		}
		result = append(result, hit)
	}

	sort.Slice(result, func(left, right int) bool {
		if result[left].Score != result[right].Score {
			return result[left].Score > result[right].Score
		}
		return result[left].ID < result[right].ID
	})

	return result, nil
}

func validateRanking(ids []string) error {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			return fmt.Errorf("%w: empty ID", ErrInvalidRanking)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("%w: duplicate ID", ErrInvalidRanking)
		}
		seen[id] = struct{}{}
	}
	return nil
}
