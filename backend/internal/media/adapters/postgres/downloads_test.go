package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/media/app"
	"github.com/jooservices/xflickr/internal/media/domain"
)

func TestDownloadsRejectsInvalidArguments(t *testing.T) {
	ctx := context.Background()
	valid := Downloads{Pool: nil}
	if _, _, err := valid.PackItems(ctx, 1); err == nil {
		t.Fatal("PackItems accepted a nil pool")
	}
	if _, _, err := (Downloads{}).PackItems(nil, 1); err == nil {
		t.Fatal("PackItems accepted a nil context")
	}
	if _, _, err := (Downloads{}).PackItems(ctx, 0); err == nil {
		t.Fatal("PackItems accepted an invalid download id")
	}
	if _, _, err := valid.PackedOriginals(ctx, 1); err == nil {
		t.Fatal("PackedOriginals accepted a nil pool")
	}
	if err := valid.SettleItem(ctx, 1, 1, "failed", nil, "failure"); err == nil {
		t.Fatal("SettleItem accepted a nil pool")
	}
	if err := valid.CompletePack(ctx, 1, "ready", []app.ZIPPart{{Number: 1, BlobKey: "part", ExpiresAt: time.Now()}}); err == nil {
		t.Fatal("CompletePack accepted a nil pool")
	}
	if _, err := valid.Create(ctx, domain.Download{}, nil); err == nil {
		t.Fatal("Create accepted a nil pool")
	}
}
