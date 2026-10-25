package postgres

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/media/adapters/local"
	"github.com/jooservices/xflickr/internal/media/app"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestPackTwentyFilesIntoThreePartsWithPostgres(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := dbtest.Open(t, downloadIntegrationDatabase(t))
	base := time.Now().UnixNano()
	userID := base
	owner := fmt.Sprintf("pack-%d@example.invalid", base)
	person := fmt.Sprintf("pack-owner-%d", base)
	downloadID := base
	photoIDs := make([]int64, 20)
	blobDir := filepath.Join(t.TempDir(), "blobs")
	blobs, err := local.New(blobDir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,password_hash,role) VALUES($1,$2,'not-used','user')`, userID, owner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=ANY($1::bigint[])`, photoIDs)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, person)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,$1)`, person); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO downloads(id,user_id,kind,subject,status,expires_at) VALUES($1,$2,'album','pack-test','pending',now()+interval '1 day')`, downloadID, userID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		photoID := base + int64(i) + 1
		photoIDs[i] = photoID
		if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,last_update,visibility,sizes,sizes_status,raw,content_hash) VALUES($1,$2,now(),'public','[]'::jsonb,'confirmed','{}',decode(repeat('ab',32),'hex'))`, photoID, person); err != nil {
			t.Fatal(err)
		}
		key := fmt.Sprintf("originals/%d", photoID)
		data := bytes.Repeat([]byte{byte(i + 1)}, 100)
		if err := blobs.Put(ctx, key, bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		var assetID int64
		if err := pool.QueryRow(ctx, `INSERT INTO assets(photo_id,size_label,sha256,bytes,blob_key,expires_at) VALUES($1,'original',decode(repeat('00',32),'hex'),$2,$3,now()+interval '1 day') RETURNING id`, photoID, len(data), key).Scan(&assetID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO download_items(download_id,photo_id,state,asset_id) VALUES($1,$2,'ready',$3)`, downloadID, photoID, assetID); err != nil {
			t.Fatal(err)
		}
	}
	packer := app.Packer{Blobs: blobs, Repo: Downloads{Pool: pool}, Limit: 3000}
	if err := packer.Pack(ctx, downloadID); err != nil {
		t.Fatal(err)
	}
	var parts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM download_parts WHERE download_id=$1`, downloadID).Scan(&parts); err != nil {
		t.Fatal(err)
	}
	if parts != 3 {
		t.Fatalf("persisted parts = %d, want 3", parts)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM downloads WHERE id=$1`, downloadID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "ready" {
		t.Fatalf("status = %q", status)
	}
	rows, err := pool.Query(ctx, `SELECT blob_key FROM download_parts WHERE download_id=$1 ORDER BY part_number`, downloadID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatal(err)
		}
		file, err := blobs.Get(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		archiveBytes, err := io.ReadAll(file)
		_ = file.Close()
		if err != nil {
			t.Fatal(err)
		}
		archive, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range archive.File {
			if entry.Name == "manifest.csv" {
				continue
			}
			count++
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 20 {
		t.Fatalf("archived files = %d, want 20", count)
	}
	for _, photoID := range photoIDs {
		if _, err := blobs.Stat(ctx, fmt.Sprintf("originals/%d", photoID)); err == nil {
			t.Errorf("original %d still exists", photoID)
		}
	}
}
