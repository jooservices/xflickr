package app

// SizesResolvedTopic carries terminal size outcomes to download consumers.
const SizesResolvedTopic = "photo.sizes_resolved"

// SizesResolved identifies a photo whose sizes can now unblock waiting work.
type SizesResolved struct {
	PhotoID int64  `json:"photo_id"`
	UserID  int64  `json:"user_id,omitempty"`
	Status  string `json:"status"`
}
