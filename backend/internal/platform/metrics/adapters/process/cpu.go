package process

import (
	"fmt"
	"strconv"
	"strings"
)

// CPUSample holds cumulative user+system jiffies from /proc/self/stat.
type CPUSample struct {
	Jiffies uint64
}

// ParseStatCPU reads fields 14 (utime) and 15 (stime) from /proc/self/stat text.
func ParseStatCPU(stat string) (CPUSample, error) {
	// comm may contain spaces and parentheses; find the last ')' then split.
	closeParen := strings.LastIndex(stat, ")")
	if closeParen < 0 || closeParen+2 >= len(stat) {
		return CPUSample{}, fmt.Errorf("metrics: invalid /proc/self/stat")
	}
	fields := strings.Fields(stat[closeParen+2:])
	// After comm, field index 0 is state (stat field 3). utime is field 14 →
	// index 11; stime is field 15 → index 12.
	if len(fields) < 13 {
		return CPUSample{}, fmt.Errorf("metrics: /proc/self/stat too short")
	}
	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return CPUSample{}, fmt.Errorf("metrics: parse utime: %w", err)
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return CPUSample{}, fmt.Errorf("metrics: parse stime: %w", err)
	}
	return CPUSample{Jiffies: utime + stime}, nil
}

// CPUPercent computes utilisation between two samples over elapsed wall time.
func CPUPercent(prev, next CPUSample, elapsedSeconds float64, clockTicks float64) float64 {
	if elapsedSeconds <= 0 || clockTicks <= 0 || next.Jiffies < prev.Jiffies {
		return 0
	}
	delta := float64(next.Jiffies - prev.Jiffies)
	return (delta / clockTicks) / elapsedSeconds * 100
}
