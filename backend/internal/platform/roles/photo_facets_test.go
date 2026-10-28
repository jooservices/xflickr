package roles

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/fakeflickr"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
	scheduleapp "github.com/jooservices/xflickr/internal/schedule/app"
	settingsapp "github.com/jooservices/xflickr/internal/settings/app"
	settingsdomain "github.com/jooservices/xflickr/internal/settings/domain"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

func TestPhotoFacetClientUsesFakeFlickrAndPhotoQuota(t *testing.T) {
	dataset, err := fakeflickr.LoadDataset("")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(fakeflickr.NewServer(dataset, nil))
	defer server.Close()

	rows := make([]pgx.Row, 5)
	for i := range rows {
		rows[i] = credentialRow{values: []any{"fake-api-key", "valid", int64(9)}}
	}
	governor := &photoFacetGovernorFake{}
	client := photoFacetClient{
		Pool:   &credentialPool{rows: rows},
		Client: flickrclient.NewClient(server.URL+"/services/rest", server.Client()),
		Quota:  governor,
	}
	ctx := context.Background()
	const photoID = "52000000001"
	if info, err := client.PhotosGetInfo(ctx, 7, photoID, "seedsecret"); err != nil || info.ID != photoID {
		t.Fatalf("PhotosGetInfo()=%+v err=%v", info, err)
	}
	if exif, err := client.PhotosGetExif(ctx, 7, photoID, "seedsecret"); err != nil || exif.Camera != "Fake Camera" {
		t.Fatalf("PhotosGetExif()=%+v err=%v", exif, err)
	}
	if _, err := client.PhotosGetAllContexts(ctx, 7, photoID); err != nil {
		t.Fatalf("PhotosGetAllContexts(): %v", err)
	}
	if comments, err := client.PhotosCommentsGetList(ctx, 7, photoID, 1); err != nil || len(comments.Items) != 0 {
		t.Fatalf("PhotosCommentsGetList()=%+v err=%v", comments, err)
	}
	if favorites, err := client.PhotosGetFavorites(ctx, 7, photoID, 1); err != nil || len(favorites.Items) != 1 {
		t.Fatalf("PhotosGetFavorites()=%+v err=%v", favorites, err)
	}
	if governor.acquired != 5 || governor.completed != 5 || governor.wrongClass {
		t.Fatalf("quota calls: acquired=%d completed=%d wrongClass=%t", governor.acquired, governor.completed, governor.wrongClass)
	}
}

func TestPhotoFacetClientCallFailures(t *testing.T) {
	boom := errors.New("database unavailable")
	validPool := func() *credentialPool {
		return &credentialPool{rows: []pgx.Row{credentialRow{values: []any{"fake-api-key", "valid", int64(9)}}}}
	}
	requestCalled := false
	request := func(context.Context, flickrclient.Credentials) error {
		requestCalled = true
		return nil
	}
	if err := (photoFacetClient{}).call(context.Background(), 7, "flickr.test.echo", "test_facet", request); err == nil {
		t.Fatal("call without dependencies succeeded")
	}
	if requestCalled {
		t.Fatal("request ran without dependencies")
	}
	clientFor := func(pool *credentialPool, governor *photoFacetGovernorFake) photoFacetClient {
		return photoFacetClient{Pool: pool, Client: flickrclient.NewClient("http://127.0.0.1", nil), Quota: governor}
	}
	if err := clientFor(&credentialPool{rows: []pgx.Row{credentialRow{err: boom}}}, &photoFacetGovernorFake{}).call(context.Background(), 7, "flickr.test.echo", "test_facet", request); !errors.Is(err, boom) {
		t.Fatalf("key query error = %v, want database error", err)
	}
	if err := clientFor(&credentialPool{rows: []pgx.Row{credentialRow{values: []any{"fake-api-key", "revoked", int64(9)}}}}, &photoFacetGovernorFake{}).call(context.Background(), 7, "flickr.test.echo", "test_facet", request); err == nil {
		t.Fatal("call with revoked key succeeded")
	}
	governor := &photoFacetGovernorFake{acquireErr: boom}
	if err := clientFor(validPool(), governor).call(context.Background(), 7, "flickr.test.echo", "test_facet", request); !errors.Is(err, boom) {
		t.Fatalf("quota acquire error = %v, want quota error", err)
	}
	governor = &photoFacetGovernorFake{noPermit: true, wait: time.Minute}
	err := clientFor(validPool(), governor).call(context.Background(), 7, "flickr.test.echo", "test_facet", request)
	var waiting *crawlapp.QuotaWaitError
	if !errors.As(err, &waiting) || !errors.Is(err, flickrclient.ErrQuotaExceeded) || waiting.Wait != time.Minute {
		t.Fatalf("no permit error=%v, want one-minute quota wait", err)
	}
	governor = &photoFacetGovernorFake{doneErr: boom}
	err = clientFor(validPool(), governor).call(context.Background(), 7, "flickr.test.echo", "test_facet", request)
	if !errors.Is(err, boom) || !requestCalled {
		t.Fatalf("permit completion error=%v requestCalled=%t", err, requestCalled)
	}
}

func TestPhotoFacetSettingsReadsApplicationDefaults(t *testing.T) {
	snapshot := settingsdomain.DefaultSnapshot()
	info := snapshot.Settings[settingsdomain.PhotoInfoEnabled]
	info.Value = json.RawMessage("false")
	snapshot.Settings[settingsdomain.PhotoInfoEnabled] = info
	comments := snapshot.Settings[settingsdomain.PhotoCommentsEnabled]
	comments.Value = json.RawMessage("true")
	snapshot.Settings[settingsdomain.PhotoCommentsEnabled] = comments
	settings := photoFacetSettings{settings: settingsapp.New(photoFacetSettingsRepository{snapshot: snapshot})}
	got, err := settings.PhotoFacets(context.Background())
	want := schedulePhotoFacetOptions(false, true, true, true, false)
	if err != nil || got != want {
		t.Fatalf("PhotoFacets()=%+v err=%v, want %+v", got, err, want)
	}
	if _, err := (photoFacetSettings{}).PhotoFacets(context.Background()); err == nil {
		t.Fatal("PhotoFacets() without settings succeeded")
	}
	if _, err := (photoFacetSettings{settings: settingsapp.New(photoFacetSettingsRepository{err: errors.New("read failed")})}).PhotoFacets(context.Background()); err == nil {
		t.Fatal("PhotoFacets() swallowed settings error")
	}
}

func schedulePhotoFacetOptions(info, exif, contexts, comments, favorites bool) scheduleapp.PhotoFacetOptions {
	return scheduleapp.PhotoFacetOptions{Info: info, Exif: exif, Contexts: contexts, Comments: comments, Favorites: favorites}
}

type photoFacetSettingsRepository struct {
	snapshot settingsdomain.Snapshot
	err      error
}

func (r photoFacetSettingsRepository) Load(context.Context, int64) (settingsdomain.Snapshot, error) {
	return r.snapshot, r.err
}

func (photoFacetSettingsRepository) SetUserOverride(context.Context, int64, settingsdomain.Key, json.RawMessage) error {
	return nil
}

func (photoFacetSettingsRepository) SetApplicationSetting(context.Context, settingsdomain.Definition) error {
	return nil
}

type photoFacetGovernorFake struct {
	acquired   int
	completed  int
	wrongClass bool
	noPermit   bool
	wait       time.Duration
	acquireErr error
	doneErr    error
}

func (g *photoFacetGovernorFake) Acquire(_ context.Context, _ int64, class quotadomain.Class) (quotadomain.Permit, time.Duration, error) {
	g.acquired++
	if class != quotadomain.Photo {
		g.wrongClass = true
	}
	if g.acquireErr != nil {
		return nil, 0, g.acquireErr
	}
	if g.noPermit {
		return nil, g.wait, nil
	}
	return photoFacetPermitFake{completed: &g.completed, err: g.doneErr}, 0, nil
}

type photoFacetPermitFake struct {
	completed *int
	err       error
}

func (p photoFacetPermitFake) Done(quotadomain.Meta) error {
	(*p.completed)++
	return p.err
}
