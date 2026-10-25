package app

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"
)

// MaxZIPPartBytes is the maximum size of a generated ZIP part (2 GiB).
const MaxZIPPartBytes int64 = 2 << 30

// PackJob is the durable queue payload for one download package.
type PackJob struct {
	DownloadID int64 `json:"download_id"`
}

// ParsePackJob validates a media.pack queue payload.
func ParsePackJob(data []byte) (PackJob, error) {
	var job PackJob
	if err := json.Unmarshal(data, &job); err != nil || job.DownloadID <= 0 {
		return PackJob{}, errors.New("media: invalid pack job")
	}
	return job, nil
}

// PackItem describes a completed original or a failed download.
type PackItem struct {
	PhotoID int64
	Name    string
	BlobKey string
	Size    int64
	SHA256  string
	Failure string
}

// ZIPPart is a published ZIP object and its temporary link expiry.
type ZIPPart struct {
	Number    int
	BlobKey   string
	Bytes     int64
	ExpiresAt time.Time
}

// PackRepository reads settled download items and atomically records their
// parts and final status. Implementations enqueue this operation via media.pack.
type PackRepository interface {
	PackItems(context.Context, int64) ([]PackItem, time.Duration, error)
	CompletePack(context.Context, int64, string, []ZIPPart) error
}

// Packer writes ready originals into stored ZIP parts and removes the source
// blobs after the database has recorded the output links.
type Packer struct {
	Blobs BlobStore
	Repo  PackRepository
	Now   func() time.Time
	Limit int64 // Optional smaller limit, primarily useful for deterministic tests.
}

// Pack streams one settled download into one or more ZIP objects.
//
//nolint:cyclop // The workflow keeps object creation, transactional completion, and source cleanup ordered.
func (p Packer) Pack(ctx context.Context, downloadID int64) error {
	if ctx == nil || p.Blobs == nil || p.Repo == nil || downloadID <= 0 {
		return errors.New("media: packer requires context, blob store, repository, and download id")
	}
	items, ttl, err := p.Repo.PackItems(ctx, downloadID)
	if err != nil {
		return fmt.Errorf("media: load pack items: %w", err)
	}
	if len(items) == 0 {
		return errors.New("media: no settled download items to pack")
	}
	limit := p.Limit
	if limit <= 0 || limit > MaxZIPPartBytes {
		limit = MaxZIPPartBytes
	}
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	files := make([]PackItem, 0, len(items))
	for _, item := range items {
		if item.Failure == "" {
			if strings.TrimSpace(item.BlobKey) == "" || item.Size < 0 {
				return fmt.Errorf("media: ready item %d has invalid blob metadata", item.PhotoID)
			}
			files = append(files, item)
		}
	}
	groups, err := splitPackItems(files, limit)
	if err != nil {
		return err
	}
	manifest, checksums, err := p.buildManifest(ctx, items)
	if err != nil {
		return err
	}
	if err := reserveManifestPart(&groups, limit, int64(len(manifest))); err != nil {
		return err
	}
	parts := make([]ZIPPart, 0, len(groups))
	for i, group := range groups {
		key := fmt.Sprintf("downloads/%d/part-%03d.zip", downloadID, i+1)
		includeManifest := i == len(groups)-1
		if err := p.writePart(ctx, key, group, manifest, checksums, includeManifest, limit); err != nil {
			return fmt.Errorf("media: write part %d: %w", i+1, err)
		}
		info, err := p.Blobs.Stat(ctx, key)
		if err != nil {
			return fmt.Errorf("media: inspect ZIP part %d: %w", i+1, err)
		}
		if info.Size > limit {
			_ = p.Blobs.Delete(ctx, key)
			return fmt.Errorf("media: ZIP part %d exceeds size limit", i+1)
		}
		parts = append(parts, ZIPPart{Number: i + 1, BlobKey: key, Bytes: info.Size, ExpiresAt: now().Add(ttl)})
	}
	status := "ready"
	for _, item := range items {
		if item.Failure != "" {
			status = "ready_with_errors"
			break
		}
	}
	if err := p.Repo.CompletePack(ctx, downloadID, status, parts); err != nil {
		return fmt.Errorf("media: record ZIP parts: %w", err)
	}
	for _, item := range files {
		if err := p.Blobs.Delete(ctx, item.BlobKey); err != nil {
			return fmt.Errorf("media: delete packed original %q: %w", item.BlobKey, err)
		}
	}
	return nil
}

func splitPackItems(items []PackItem, limit int64) ([][]PackItem, error) {
	if limit <= 0 {
		return nil, errors.New("media: ZIP part limit must be positive")
	}
	if len(items) == 0 {
		return [][]PackItem{{}}, nil
	}
	var groups [][]PackItem
	group := make([]PackItem, 0)
	projected := int64(22) // End of central directory.
	central := int64(0)
	for _, item := range items {
		name := item.Name
		if name == "" {
			name = strconv.FormatInt(item.PhotoID, 10)
		}
		name = path.Base(strings.ReplaceAll(name, "\\", "/"))
		header := int64(30 + len(name) + 16) // Local header and data descriptor.
		entryCentral := int64(46 + len(name))
		if header+entryCentral+22 > limit || item.Size > limit-header-entryCentral-22 {
			return nil, fmt.Errorf("media: item %d cannot fit in a ZIP part", item.PhotoID)
		}
		if len(group) > 0 && projected+header+item.Size+central+entryCentral > limit {
			groups = append(groups, group)
			group = make([]PackItem, 0)
			projected, central = 22, 0
		}
		group = append(group, item)
		projected += header + item.Size
		central += entryCentral
	}
	groups = append(groups, group)
	return groups, nil
}

func reserveManifestPart(groups *[][]PackItem, limit, manifestBytes int64) error {
	const manifestZIPOverhead = int64(30 + len("manifest.csv") + 16 + 46 + len("manifest.csv"))
	manifestCost := manifestBytes + manifestZIPOverhead
	if manifestCost+22 > limit {
		return errors.New("media: manifest cannot fit in a ZIP part")
	}
	lastIndex := len(*groups) - 1
	last := (*groups)[lastIndex]
	if estimatedPartSize(last)+manifestCost <= limit {
		return nil
	}
	if len(last) <= 1 {
		*groups = append(*groups, []PackItem{})
		return nil
	}
	// Move only the tail needed to give the final part room for the complete
	// manifest. This avoids an otherwise empty manifest-only part.
	moved := make([]PackItem, 0, len(last))
	for len(last)-len(moved) > 1 {
		candidate := append([]PackItem{last[len(last)-len(moved)-1]}, moved...)
		if estimatedPartSize(candidate)+manifestCost <= limit {
			moved = candidate
			break
		}
		moved = candidate
	}
	if len(moved) == 0 || estimatedPartSize(moved)+manifestCost > limit {
		*groups = append(*groups, []PackItem{})
		return nil
	}
	(*groups)[lastIndex] = last[:len(last)-len(moved)]
	*groups = append(*groups, moved)
	return nil
}

func estimatedPartSize(items []PackItem) int64 {
	size, central := int64(22), int64(0)
	for _, item := range items {
		name := item.Name
		if name == "" {
			name = strconv.FormatInt(item.PhotoID, 10)
		}
		name = path.Base(strings.ReplaceAll(name, "\\", "/"))
		size += int64(30+len(name)+16) + item.Size
		central += int64(46 + len(name))
	}
	return size + central
}

func (p Packer) buildManifest(ctx context.Context, items []PackItem) ([]byte, map[int64]string, error) {
	var output strings.Builder
	w := csv.NewWriter(&output)
	if err := w.Write([]string{"photo_id", "file", "sha256", "failure"}); err != nil {
		return nil, nil, err
	}
	checksums := make(map[int64]string, len(items))
	for _, item := range items {
		name := item.Name
		if name == "" {
			name = strconv.FormatInt(item.PhotoID, 10)
		}
		name = path.Base(strings.ReplaceAll(name, "\\", "/"))
		sum := item.SHA256
		if item.Failure == "" {
			blob, err := p.Blobs.Get(ctx, item.BlobKey)
			if err != nil {
				return nil, nil, fmt.Errorf("media: read original %q for checksum: %w", item.BlobKey, err)
			}
			h := sha256.New()
			_, copyErr := io.Copy(h, &contextReader{ctx: ctx, reader: blob})
			closeErr := blob.Close()
			if err := errors.Join(copyErr, closeErr); err != nil {
				return nil, nil, fmt.Errorf("media: checksum original %q: %w", item.BlobKey, err)
			}
			sum = fmt.Sprintf("%x", h.Sum(nil))
			checksums[item.PhotoID] = sum
		}
		if err := w.Write([]string{strconv.FormatInt(item.PhotoID, 10), name, sum, item.Failure}); err != nil {
			return nil, nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, nil, err
	}
	return []byte(output.String()), checksums, nil
}

func (p Packer) writePart(ctx context.Context, key string, items []PackItem, manifest []byte, checksums map[int64]string, includeManifest bool, limit int64) error {
	reader, writer := io.Pipe()
	writeErr := make(chan error, 1)
	go func() {
		zipper := zip.NewWriter(writer)
		var err error
		for _, item := range items {
			name := item.Name
			if name == "" {
				name = strconv.FormatInt(item.PhotoID, 10)
			}
			name = path.Base(strings.ReplaceAll(name, "\\", "/"))
			header := &zip.FileHeader{Name: name, Method: zip.Store}
			header.SetMode(0o600)
			entry, createErr := zipper.CreateHeader(header)
			if createErr != nil {
				err = createErr
				break
			}
			blob, getErr := p.Blobs.Get(ctx, item.BlobKey)
			if getErr != nil {
				err = getErr
				break
			}
			h := sha256.New()
			_, copyErr := io.Copy(entry, io.TeeReader(&contextReader{ctx: ctx, reader: blob}, h))
			closeErr := blob.Close()
			if copyErr != nil || closeErr != nil {
				err = errors.Join(copyErr, closeErr)
				break
			}
			if sum := fmt.Sprintf("%x", h.Sum(nil)); sum != checksums[item.PhotoID] {
				err = fmt.Errorf("media: original %d changed while packing", item.PhotoID)
				break
			}
		}
		if err == nil && includeManifest {
			entry, createErr := zipper.CreateHeader(&zip.FileHeader{Name: "manifest.csv", Method: zip.Store})
			if createErr != nil {
				err = createErr
			} else {
				_, err = entry.Write(manifest)
			}
		}
		if closeErr := zipper.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		_ = writer.CloseWithError(err)
		writeErr <- err
	}()
	putErr := p.Blobs.Put(ctx, key, &limitReader{reader: reader, remaining: limit + 1})
	_ = reader.Close()
	producerErr := <-writeErr
	if err := errors.Join(putErr, producerErr); err != nil {
		_ = p.Blobs.Delete(context.Background(), key)
		return err
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(buffer)
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("media: read original blob: %w", err)
	}
	return n, nil
}

type limitReader struct {
	reader    io.Reader
	remaining int64
}

func (r *limitReader) Read(buffer []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, errors.New("media: ZIP part exceeds size limit")
	}
	if int64(len(buffer)) > r.remaining {
		buffer = buffer[:r.remaining]
	}
	n, err := r.reader.Read(buffer)
	r.remaining -= int64(n)
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("media: read ZIP stream: %w", err)
	}
	return n, nil
}
