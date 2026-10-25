package app

import (
	"errors"
	"sort"
)

// ShareGroup describes a plan's fraction and available due backlog.
type ShareGroup struct {
	ID        int64
	Share     float64
	Available int
}

// SplitShare allocates capacity by configured share, then lends any unused
// allocation to groups with remaining work in proportion to their shares.
// Fractional units use the largest-remainder method; ties prefer the larger
// share, then rotate through stable plan IDs using the optional rotation.
func SplitShare(capacity int, groups []ShareGroup, rotation ...int) (map[int64]int, error) {
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
	active := append([]ShareGroup(nil), groups...)
	for remaining > 0 && len(active) > 0 {
		rotationOffset := 0
		if len(rotation) > 0 {
			rotationOffset = rotation[0]
		}
		allocationsThisRound := largestRemainder(remaining, active, rotationOffset)
		allocated := 0
		next := make([]ShareGroup, 0, len(active))
		for _, group := range active {
			allocation := min(allocationsThisRound[group.ID], group.Available-allocations[group.ID])
			allocations[group.ID] += allocation
			allocated += allocation
			if allocations[group.ID] < group.Available {
				next = append(next, group)
			}
		}
		remaining -= allocated
		if allocated == 0 {
			break
		}
		active = next
	}
	return allocations, nil
}

func largestRemainder(capacity int, groups []ShareGroup, rotation int) map[int64]int {
	result := make(map[int64]int, len(groups))
	shareTotal := 0.0
	for _, group := range groups {
		shareTotal += group.Share
	}
	if shareTotal == 0 {
		return result
	}
	type remainder struct {
		id       int64
		fraction float64
		share    float64
	}
	remainders := make([]remainder, 0, len(groups))
	ids := make([]int64, 0, len(groups))
	allocated := 0
	for _, group := range groups {
		quota := float64(capacity) * group.Share / shareTotal
		whole := int(quota)
		result[group.ID] = whole
		allocated += whole
		remainders = append(remainders, remainder{id: group.ID, fraction: quota - float64(whole), share: group.Share})
		ids = append(ids, group.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	rank := make(map[int64]int, len(ids))
	if len(ids) > 0 {
		rotation %= len(ids)
		if rotation < 0 {
			rotation += len(ids)
		}
		for i, id := range ids {
			rank[id] = (i - rotation + len(ids)) % len(ids)
		}
	}
	sort.Slice(remainders, func(i, j int) bool {
		if remainders[i].fraction != remainders[j].fraction {
			return remainders[i].fraction > remainders[j].fraction
		}
		if remainders[i].share != remainders[j].share {
			return remainders[i].share > remainders[j].share
		}
		return rank[remainders[i].id] < rank[remainders[j].id]
	})
	for i := 0; allocated < capacity && i < len(remainders); i++ {
		if remainders[i].share == 0 {
			continue
		}
		result[remainders[i].id]++
		allocated++
	}
	return result
}
