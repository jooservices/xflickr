package app_test

import (
	"testing"

	"github.com/jooservices/xflickr/internal/media/app"
	"github.com/jooservices/xflickr/internal/media/blobstoretest"
)

// TestInMemoryBlobFakesMeetContract runs the shared blob store contract against
// every in-memory fake that the app tests use. The fakes are constructed through
// export_test.go because the contract imports app.
func TestInMemoryBlobFakesMeetContract(t *testing.T) {
	fakes := []struct {
		name    string
		newFake func() app.BlobStore
	}{
		{"pack", app.NewPackBlobStoreForTest},
		{"fake blobs", app.NewFakeBlobsForTest},
		{"download links", app.NewDownloadLinksBlobFakeForTest},
	}
	for _, fake := range fakes {
		t.Run(fake.name, func(t *testing.T) {
			blobstoretest.Run(t, func(*testing.T) app.BlobStore { return fake.newFake() })
		})
	}
}
