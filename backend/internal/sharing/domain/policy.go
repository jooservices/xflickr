package domain

// User identifies the XFlickr user whose access is being evaluated.
type User struct {
	ID int64
}

// Observer records whether a user observed a record and opted in to sharing.
type Observer struct {
	UserID         int64
	SharingEnabled bool
}

// Record contains only the visibility facts needed by the sharing policy.
// Personal data such as credentials, downloads, and sync targets is not a
// record governed by this policy.
type Record struct {
	Visibility string
	Observers  []Observer
}

// IndexVisibility is the visibility projection stored by search indexers.
// VisibleTo contains users with a direct observation. PubliclyShared allows
// every XFlickr user to see a public record contributed by a sharing observer.
type IndexVisibility struct {
	VisibleTo      []int64 `json:"visible_to"`
	PubliclyShared bool    `json:"publicly_shared"`
}

// CanSee reports whether user may see record under the sharing policy.
func CanSee(user User, record Record) bool {
	observed := false
	sharedObserver := false
	for _, observer := range record.Observers {
		if observer.UserID == user.ID {
			observed = true
		}
		if observer.SharingEnabled {
			sharedObserver = true
		}
	}
	return observed || (record.Visibility == "public" && sharedObserver)
}

// VisibleTo computes the indexer's direct-visibility projection and whether a
// public record is shared. It deliberately does not include personal data.
func VisibleTo(record Record) IndexVisibility {
	result := IndexVisibility{VisibleTo: make([]int64, 0, len(record.Observers))}
	seen := make(map[int64]struct{}, len(record.Observers))
	for _, observer := range record.Observers {
		if _, exists := seen[observer.UserID]; !exists {
			result.VisibleTo = append(result.VisibleTo, observer.UserID)
			seen[observer.UserID] = struct{}{}
		}
		if observer.SharingEnabled && record.Visibility == "public" {
			result.PubliclyShared = true
		}
	}
	return result
}
