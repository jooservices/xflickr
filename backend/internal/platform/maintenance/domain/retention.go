// Package domain holds api_call_log retention rules with no infrastructure
// imports.
package domain

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultRetentionDays is the seeded admin default for call-log retention.
	DefaultRetentionDays = 30
	// MinRetentionDays is the lowest allowed retention window.
	MinRetentionDays = 7
	// MaxRetentionDays is the highest allowed retention window.
	MaxRetentionDays = 365
	// DailyPartitionPrefix is the naming prefix for RANGE partitions.
	DailyPartitionPrefix = "api_call_log_"
)

// DailyPartition is one RANGE(called_at) child of api_call_log.
type DailyPartition struct {
	Name string
	Day  time.Time // UTC midnight of the partition's lower bound
}

// UpperBound is the exclusive upper timestamptz of the partition day.
func (p DailyPartition) UpperBound() time.Time {
	return p.Day.AddDate(0, 0, 1)
}

// ParseDailyPartitionName extracts the UTC day from api_call_log_YYYYMMDD.
func ParseDailyPartitionName(name string) (DailyPartition, error) {
	if !strings.HasPrefix(name, DailyPartitionPrefix) {
		return DailyPartition{}, fmt.Errorf("maintenance: partition name %q missing prefix", name)
	}
	suffix := strings.TrimPrefix(name, DailyPartitionPrefix)
	if len(suffix) != 8 {
		return DailyPartition{}, fmt.Errorf("maintenance: partition name %q is not a daily stamp", name)
	}
	if _, err := strconv.Atoi(suffix); err != nil {
		return DailyPartition{}, fmt.Errorf("maintenance: partition name %q is not numeric: %w", name, err)
	}
	day, err := time.ParseInLocation("20060102", suffix, time.UTC)
	if err != nil {
		return DailyPartition{}, fmt.Errorf("maintenance: parse partition day %q: %w", name, err)
	}
	return DailyPartition{Name: name, Day: day}, nil
}

// ClampRetentionDays enforces the admin bounds, substituting the default when
// the value is non-positive.
func ClampRetentionDays(days int) int {
	if days <= 0 {
		return DefaultRetentionDays
	}
	if days < MinRetentionDays {
		return MinRetentionDays
	}
	if days > MaxRetentionDays {
		return MaxRetentionDays
	}
	return days
}

// RetentionCutoff is the UTC midnight of (now - retentionDays). Partitions
// whose upper bound is at or before this instant are eligible for drop.
func RetentionCutoff(now time.Time, retentionDays int) time.Time {
	days := ClampRetentionDays(retentionDays)
	current := now.UTC()
	today := time.Date(current.Year(), current.Month(), current.Day(), 0, 0, 0, 0, time.UTC)
	return today.AddDate(0, 0, -days)
}

// ChooseExpired returns daily partitions whose exclusive upper bound is older
// than or equal to the cutoff (upper <= cutoff), oldest first. Current and
// future days are never selected.
func ChooseExpired(partitions []DailyPartition, cutoff time.Time) []DailyPartition {
	cutoff = cutoff.UTC()
	var out []DailyPartition
	for _, part := range partitions {
		if part.Day.IsZero() || part.Name == "" {
			continue
		}
		if !part.UpperBound().After(cutoff) {
			out = append(out, part)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day.Before(out[j].Day) })
	return out
}
