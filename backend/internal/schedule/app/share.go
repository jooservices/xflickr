package app

import "errors"

// ShareGroup describes a plan's fraction and available due backlog.
type ShareGroup struct {
	ID        int64
	Share     float64
	Available int
}

// SplitShare allocates capacity by configured share, then lends any unused
// allocation to groups with remaining work in proportion to their shares.
func SplitShare(capacity int, groups []ShareGroup) (map[int64]int, error) {
	if capacity < 0 {
		return nil, errors.New("schedule: capacity cannot be negative")
	}
	allocations := make(map[int64]int, len(groups))
	if len(groups) == 0 {
		return allocations, nil
	}
	totalShare := 0.0
	for _, group := range groups {
		if group.Share < 0 || group.Available < 0 {
			return nil, errors.New("schedule: invalid share group")
		}
		totalShare += group.Share
	}
	if totalShare <= 0 {
		return nil, errors.New("schedule: plan shares must total above zero")
	}
	remaining := capacity
	for _, group := range groups {
		allocation := int(float64(capacity) * group.Share / totalShare)
		allocation = min(allocation, group.Available)
		allocations[group.ID] += allocation
		remaining -= allocation
	}
	for remaining > 0 {
		progress := false
		for _, group := range groups {
			if group.Share == 0 || allocations[group.ID] >= group.Available {
				continue
			}
			allocations[group.ID]++
			remaining--
			progress = true
			if remaining == 0 {
				break
			}
		}
		if !progress {
			break
		}
	}
	return allocations, nil
}
