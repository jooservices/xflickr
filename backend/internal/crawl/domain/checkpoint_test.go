package domain

import "testing"

func TestNextCheckpointTable(t *testing.T) {
	const (
		scanStart ScanCheckpoint = 1_700_000_000
		oldZero   ScanCheckpoint = 0
		oldValue  ScanCheckpoint = 1_600_000_000
		boundary  ScanCheckpoint = 1_700_000_000
	)
	tests := []struct {
		name           string
		scanStart      ScanCheckpoint
		pagesCommitted bool
		old            ScanCheckpoint
		want           ScanCheckpoint
	}{
		{name: "initial success stores scan start", scanStart: scanStart, pagesCommitted: true, old: oldZero, want: scanStart},
		{name: "partial scan keeps old zero", scanStart: scanStart, pagesCommitted: false, old: oldZero, want: oldZero},
		{name: "failed page keeps old non-zero", scanStart: scanStart, pagesCommitted: false, old: oldValue, want: oldValue},
		{name: "inclusive boundary uses scan start as-is", scanStart: boundary, pagesCommitted: true, old: oldValue, want: boundary},
		{name: "old non-zero kept when scan start missing", scanStart: 0, pagesCommitted: true, old: oldValue, want: oldValue},
		{name: "successful rescan replaces old", scanStart: scanStart, pagesCommitted: true, old: oldValue, want: scanStart},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NextCheckpoint(tc.scanStart, tc.pagesCommitted, tc.old)
			if got != tc.want {
				t.Fatalf("NextCheckpoint(%d, %v, %d) = %d, want %d", tc.scanStart, tc.pagesCommitted, tc.old, got, tc.want)
			}
		})
	}
}

func TestNeedsScanStartCheckpoint(t *testing.T) {
	if !NeedsScanStartCheckpoint(FacetPhotos) || !NeedsScanStartCheckpoint(FacetFavorites) || !NeedsScanStartCheckpoint(FacetContacts) {
		t.Fatal("photos, favorites, and contacts must need a scan-start checkpoint")
	}
	if NeedsScanStartCheckpoint("albums") || NeedsScanStartCheckpoint("check") {
		t.Fatal("other facets must not stamp a scan-start checkpoint")
	}
}

func TestIncrementalPageUsesCheckpointFilter(t *testing.T) {
	checkpoint := ScanCheckpoint(1_700_000_000)
	photos := PhotosCheckpointFilter(checkpoint)
	if photos[ParamMinUploadDate] != "1700000000" || len(photos) != 1 {
		t.Fatalf("photos filter = %#v", photos)
	}
	favorites := FavoritesCheckpointFilter(checkpoint)
	if favorites[ParamMinFaveDate] != "1700000000" || len(favorites) != 1 {
		t.Fatalf("favorites filter = %#v", favorites)
	}
}
