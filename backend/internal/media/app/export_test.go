package app

// The in-memory fakes are unexported and the contract suite imports this
// package, so the internal tests cannot call it. These constructors let the
// external test in blobstore_contract_test.go run the suite against each fake.

// NewPackBlobStoreForTest returns the in-memory fake used by the pack tests.
func NewPackBlobStoreForTest() BlobStore { return &packBlobStore{} }

// NewFakeBlobsForTest returns the fake used by the cleaner tests.
func NewFakeBlobsForTest() BlobStore { return &fakeBlobs{} }

// NewDownloadLinksBlobFakeForTest returns the fake used by the download link tests.
func NewDownloadLinksBlobFakeForTest() BlobStore { return &downloadLinksBlobFake{} }
