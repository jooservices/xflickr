package app

import (
	"context"
	"errors"
	"testing"
)

type downloadPhotosFake struct {
	kind, subject string
	ids           []int64
	err           error
}

func (f *downloadPhotosFake) DownloadCandidates(_ context.Context, kind, subject string, ids []int64) ([]DownloadCandidate, error) {
	f.kind, f.subject, f.ids = kind, subject, ids
	return []DownloadCandidate{{PhotoID: 1}}, f.err
}

func TestDownloadCatalogDelegatesAndPropagatesErrors(t *testing.T) {
	repository := &downloadPhotosFake{}
	catalog := DownloadCatalog{Photos: repository}
	ids := []int64{1, 2}
	candidates, err := catalog.Candidates(context.Background(), "album", "a", ids)
	if err != nil || len(candidates) != 1 || repository.kind != "album" || repository.subject != "a" || len(repository.ids) != 2 {
		t.Fatalf("candidates=%+v repository=%+v err=%v", candidates, repository, err)
	}
	want := errors.New("database")
	repository.err = want
	if _, err := catalog.Candidates(context.Background(), "tray", "", ids); !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
	if _, err := (DownloadCatalog{}).Candidates(context.Background(), "tray", "", ids); err == nil {
		t.Fatal("accepted missing repository")
	}
	if _, err := catalog.Candidates(nil, "tray", "", ids); err == nil {
		t.Fatal("accepted nil context")
	}
}
