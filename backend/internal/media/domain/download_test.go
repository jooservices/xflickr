package domain

import (
	"errors"
	"testing"
)

func TestDownloadItemTransitions(t *testing.T) {
	asset := int64(7)
	cases := []struct {
		from, to ItemState
		asset    *int64
		failure  string
		wantErr  bool
		attempts int
	}{
		{ItemPending, ItemWaitingSizes, nil, "", false, 0},
		{ItemPending, ItemFetching, nil, "", false, 1},
		{ItemWaitingSizes, ItemFetching, nil, "", false, 1},
		{ItemFetching, ItemReady, &asset, "", false, 0},
		{ItemFetching, ItemFailed, nil, "network", false, 0},
		{ItemReady, ItemFetching, nil, "", true, 0},
		{ItemFetching, ItemReady, nil, "", true, 0},
		{ItemFetching, ItemFailed, nil, "", true, 0},
	}
	for _, tc := range cases {
		item := DownloadItem{PhotoID: 4, State: tc.from}
		updated, err := item.Transition(tc.to, tc.asset, tc.failure)
		if (err != nil) != tc.wantErr {
			t.Fatalf("%s -> %s error=%v", tc.from, tc.to, err)
		}
		if tc.wantErr {
			if !errors.Is(err, ErrInvalidTransition) {
				t.Fatal(err)
			}
			continue
		}
		if updated.State != tc.to || updated.Attempts != tc.attempts {
			t.Fatalf("transition = %+v", updated)
		}
	}
}

func TestDownloadValidation(t *testing.T) {
	valid := Download{UserID: 1, Kind: DownloadAlbum, Subject: "album", Items: []DownloadItem{{PhotoID: 2, State: ItemPending}}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []Download{{}, {UserID: 1, Kind: "bad", Subject: "x"}, {UserID: 1, Kind: DownloadTray, Subject: "x"}} {
		if !errors.Is(invalid.Validate(), ErrInvalidDownload) {
			t.Fatalf("accepted %+v", invalid)
		}
	}
}
