package domain

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// ScoreWeights are administrator-configurable coefficients for work priority.
type ScoreWeights struct {
	PlanWeight   float64
	DirectAction float64
	Starred      float64
	Depth        float64
	AgeMinutes   float64
}

// ScoreInput contains the work attributes used by the minutes-aware formula.
// Prefer StaticScore for persisted ordering keys; Score remains for equivalence
// proofs against the historical age-linear formula.
type ScoreInput struct {
	PlanWeight   int
	DirectAction bool
	Starred      bool
	Depth        int
	MaxDepth     int
	AgeMinutes   float64
}

// StaticScoreInput holds the time-invariant inputs for the persisted sort key.
// CreatedAt replaces AgeMinutes so the key need not be rewritten as time passes.
type StaticScoreInput struct {
	PlanWeight   int
	DirectAction bool
	Starred      bool
	Depth        int
	MaxDepth     int
	CreatedAt    time.Time
}

// Score computes the historical minutes-aware priority. Ordering by Score at any
// shared "now" equals ordering by StaticScore because the age term is affine in
// now with the same slope for every row.
func Score(input ScoreInput, weights ScoreWeights) (float64, error) {
	if err := validateScore(input, weights); err != nil {
		return 0, err
	}
	base, err := baseScore(input.PlanWeight, input.DirectAction, input.Starred, input.Depth, input.MaxDepth, weights)
	if err != nil {
		return 0, err
	}
	score := base + input.AgeMinutes*weights.AgeMinutes
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return 0, errors.New("schedule: score result must be finite")
	}
	return score, nil
}

// StaticScore returns the persisted priority key used only for ordering. Its
// absolute value is not a minutes-aware score; only relative order matters.
//
//	Score(now) = base + wAge*(now-created)/60
//	           = (base - wAge*epoch(created)/60) + wAge*now/60
//
// The last term is identical for every row at a shared now, so ordering by the
// parenthesised static key matches ordering by Score(now).
func StaticScore(input StaticScoreInput, weights ScoreWeights) (float64, error) {
	if input.CreatedAt.IsZero() {
		return 0, errors.New("schedule: static score created_at is required")
	}
	aged := ScoreInput{
		PlanWeight: input.PlanWeight, DirectAction: input.DirectAction, Starred: input.Starred,
		Depth: input.Depth, MaxDepth: input.MaxDepth,
	}
	if err := validateScore(aged, weights); err != nil {
		return 0, err
	}
	base, err := baseScore(input.PlanWeight, input.DirectAction, input.Starred, input.Depth, input.MaxDepth, weights)
	if err != nil {
		return 0, err
	}
	score := base - weights.AgeMinutes*float64(input.CreatedAt.UTC().Unix())/60.0
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return 0, errors.New("schedule: score result must be finite")
	}
	return score, nil
}

// WeightsFingerprint is a stable token of the admin score weights and max depth.
// A changed fingerprint triggers one set-based pending rescore.
func WeightsFingerprint(weights ScoreWeights, maxDepth int) string {
	return fmt.Sprintf("v1|%g|%g|%g|%g|%g|%d",
		weights.PlanWeight, weights.DirectAction, weights.Starred, weights.Depth, weights.AgeMinutes, maxDepth)
}

func baseScore(planWeight int, directAction, starred bool, depth, maxDepth int, weights ScoreWeights) (float64, error) {
	remaining := maxDepth - depth
	var direct, star float64
	if directAction {
		direct = 1
	}
	if starred {
		star = 1
	}
	score := float64(planWeight)*weights.PlanWeight + direct*weights.DirectAction + star*weights.Starred + float64(remaining)*weights.Depth
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return 0, errors.New("schedule: score result must be finite")
	}
	return score, nil
}

func validateScore(input ScoreInput, weights ScoreWeights) error {
	values := []float64{float64(input.PlanWeight), float64(input.Depth), float64(input.MaxDepth), input.AgeMinutes, weights.PlanWeight, weights.DirectAction, weights.Starred, weights.Depth, weights.AgeMinutes}
	for _, value := range values {
		if value < 0 {
			return errors.New("schedule: score values cannot be negative")
		}
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("schedule: score values must be finite")
		}
	}
	return nil
}
