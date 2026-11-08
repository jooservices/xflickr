// Package domain defines the operations metrics sample model and ports.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"
)

// Sample is one metric observation at minute resolution.
type Sample struct {
	Name   string
	Labels map[string]string
	At     time.Time
	Value  float64
}

// BucketFloor truncates t to the start of its UTC minute.
func BucketFloor(t time.Time) time.Time {
	utc := t.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), utc.Hour(), utc.Minute(), 0, 0, time.UTC)
}

// HourFloor truncates t to the start of its UTC hour.
func HourFloor(t time.Time) time.Time {
	utc := t.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), utc.Hour(), 0, 0, 0, time.UTC)
}

// LabelsHash returns a stable SHA-256 hex digest of the label set.
func LabelsHash(labels map[string]string) (string, error) {
	canonical, err := CanonicalLabelsJSON(labels)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// CanonicalLabelsJSON encodes labels with sorted keys for hashing and storage.
func CanonicalLabelsJSON(labels map[string]string) ([]byte, error) {
	if labels == nil {
		labels = map[string]string{}
	}
	keys := slices.Collect(maps.Keys(labels))
	slices.Sort(keys)
	ordered := make(map[string]string, len(keys))
	for _, key := range keys {
		ordered[key] = labels[key]
	}
	encoded, err := json.Marshal(ordered)
	if err != nil {
		return nil, fmt.Errorf("metrics: encode labels: %w", err)
	}
	return encoded, nil
}

// CloneLabels returns a shallow copy of labels.
func CloneLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return map[string]string{}
	}
	return maps.Clone(labels)
}
