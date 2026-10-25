package domain

import (
	"errors"
	"math"
)

// ScoreWeights are administrator-configurable coefficients for work priority.
type ScoreWeights struct {
	PlanWeight   float64
	DirectAction float64
	Starred      float64
	Depth        float64
	AgeMinutes   float64
}

// ScoreInput contains the work attributes used to rank an item.
type ScoreInput struct {
	PlanWeight   int
	DirectAction bool
	Starred      bool
	Depth        int
	MaxDepth     int
	AgeMinutes   float64
}

// Score computes a deterministic priority score without infrastructure access.
func Score(input ScoreInput, weights ScoreWeights) (float64, error) {
	if err := validateScore(input, weights); err != nil {
		return 0, err
	}
	depth := input.MaxDepth - input.Depth
	var direct, starred float64
	if input.DirectAction {
		direct = 1
	}
	if input.Starred {
		starred = 1
	}
	score := float64(input.PlanWeight)*weights.PlanWeight + direct*weights.DirectAction + starred*weights.Starred + float64(depth)*weights.Depth + input.AgeMinutes*weights.AgeMinutes
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
