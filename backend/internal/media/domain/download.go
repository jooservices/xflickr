package domain

import (
	"errors"
	"time"
)

type DownloadKind string

const (
	DownloadContact DownloadKind = "contact"
	DownloadAlbum   DownloadKind = "album"
	DownloadTray    DownloadKind = "tray"
)

type ItemState string

const (
	ItemPending      ItemState = "pending"
	ItemWaitingSizes ItemState = "waiting_sizes"
	ItemFetching     ItemState = "fetching"
	ItemReady        ItemState = "ready"
	ItemFailed       ItemState = "failed"
)

var (
	ErrInvalidDownload   = errors.New("media: invalid download")
	ErrInvalidTransition = errors.New("media: invalid download item transition")
)

type Download struct {
	ID        int64
	UserID    int64
	Kind      DownloadKind
	Subject   string
	Status    string
	CreatedAt time.Time
	ExpiresAt time.Time
	Items     []DownloadItem
	Skipped   int
}

type DownloadItem struct {
	DownloadID int64
	PhotoID    int64
	State      ItemState
	Attempts   int
	AssetID    *int64
	Error      string
}

func (d Download) Validate() error {
	if d.UserID <= 0 || d.Subject == "" {
		return ErrInvalidDownload
	}
	switch d.Kind {
	case DownloadContact, DownloadAlbum, DownloadTray:
	default:
		return ErrInvalidDownload
	}
	if len(d.Items) == 0 && d.Skipped == 0 {
		return ErrInvalidDownload
	}
	for _, item := range d.Items {
		if item.PhotoID <= 0 || !validState(item.State) {
			return ErrInvalidDownload
		}
	}
	return nil
}

func (i DownloadItem) Transition(next ItemState, assetID *int64, failure string) (DownloadItem, error) {
	if !allowedTransition(i.State, next) || !validState(next) || !validTransitionData(next, assetID, failure) {
		return i, ErrInvalidTransition
	}
	i.State = next
	if next == ItemFetching {
		i.Attempts++
	}
	i.AssetID = assetID
	i.Error = failure
	return i, nil
}

func allowedTransition(from, to ItemState) bool {
	transitions := map[ItemState]map[ItemState]bool{
		ItemPending:      {ItemWaitingSizes: true, ItemFetching: true, ItemFailed: true},
		ItemWaitingSizes: {ItemFetching: true, ItemFailed: true},
		ItemFetching:     {ItemReady: true, ItemFailed: true},
		ItemFailed:       {ItemWaitingSizes: true, ItemFetching: true},
	}
	return transitions[from][to]
}

func validTransitionData(next ItemState, assetID *int64, failure string) bool {
	if next == ItemReady && (assetID == nil || *assetID <= 0) {
		return false
	}
	return next != ItemFailed || failure != ""
}

func validState(state ItemState) bool {
	switch state {
	case ItemPending, ItemWaitingSizes, ItemFetching, ItemReady, ItemFailed:
		return true
	}
	return false
}
