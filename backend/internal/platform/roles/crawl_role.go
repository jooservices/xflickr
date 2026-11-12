package roles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	accountsapp "github.com/jooservices/xflickr/internal/accounts/app"
	accountsdomain "github.com/jooservices/xflickr/internal/accounts/domain"
	crawlhttp "github.com/jooservices/xflickr/internal/crawl/adapters/http"
	crawlpostgres "github.com/jooservices/xflickr/internal/crawl/adapters/postgres"
	crawlsettings "github.com/jooservices/xflickr/internal/crawl/adapters/settings"
	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/outbox"
	"github.com/jooservices/xflickr/internal/platform/queue"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
	scheduleriver "github.com/jooservices/xflickr/internal/schedule/adapters/river"
	scheduleapp "github.com/jooservices/xflickr/internal/schedule/app"
	settingspostgres "github.com/jooservices/xflickr/internal/settings/adapters/postgres"
	settingsapp "github.com/jooservices/xflickr/internal/settings/app"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

type crawlRole struct{}

func (crawlRole) Name() string { return "crawl" }

func (crawlRole) Run(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil {
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := openRoleDatabase(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()
	callObserver, stopCallObserver := startFlickrCallLog(ctx, pool)
	defer stopCallObserver()
	settingsRepository, err := settingspostgres.New(pool)
	if err != nil {
		return err
	}
	settingsService := settingsapp.New(settingsRepository)
	interestsRepository, err := crawlpostgres.New(pool)
	if err != nil {
		return err
	}
	redisClient := newRedisClient(cfg)
	defer redisClient.Close()
	stopMetrics := startMetricsCollection(ctx, pool, redisClient, cfg, "crawl", "", false, slog.Default())
	defer stopMetrics()
	queueAdapter, err := newRiverWithSettings(ctx, pool, map[string]int{scheduleriver.PhotoQueue: crawlWorkers, scheduleriver.PhotoExtrasQueue: 1, accountsapp.VerifyQueue: 1})
	if err != nil {
		return err
	}
	client := newFlickrClient(cfg, callObserver)
	engine, err := newCrawlEngine(pool, redisClient, cfg, client)
	if err != nil {
		return err
	}
	if err := registerCrawlJobs(queueAdapter, engine); err != nil {
		return err
	}
	if err := registerPhotoSizes(queueAdapter, pool, redisClient, cfg, client); err != nil {
		return err
	}
	verificationRepository, verifier, err := newAccountVerifier(pool, redisClient, cfg, callObserver)
	if err != nil {
		return err
	}
	seeder := accountsapp.VerificationSeeder{Repository: verificationRepository, Jobs: queueAdapter}
	if err := startAccountVerification(ctx, queueAdapter, verifier, seeder); err != nil {
		return err
	}
	service := crawlapp.Interests{Repository: interestsRepository, Settings: settingsService}
	configureRoleReadiness(ctx, "crawl", postgresDependency(pool), redisDependency(redisClient))
	// The River worker and the interest consumer run side by side; if either
	// fails the role fails, so a dead worker cannot leave the process idling.
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return queue.Worker{Queue: queueAdapter}.Run(groupCtx) })
	group.Go(func() error {
		return outbox.NewConsumer(pool).Consume(groupCtx, accountsdomain.ConnectedTopic, func(ctx context.Context, event outbox.Event) error {
			if err := service.HandleConnected(ctx, event.Payload); err != nil {
				return err
			}
			var connected accountsdomain.Connected
			if err := json.Unmarshal(event.Payload, &connected); err != nil {
				return fmt.Errorf("crawl: decode connected account event: %w", err)
			}
			return seeder.Enqueue(ctx, connected.AccountID)
		})
	})
	return group.Wait()
}

type photoFacetClient struct {
	Pool interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}
	Client *flickrclient.Client
	Quota  quotadomain.Governor
	NodeID string
}

func (c photoFacetClient) PhotosGetInfo(ctx context.Context, keyID int64, photoID, secret string) (flickrclient.PhotoInfo, error) {
	var result flickrclient.PhotoInfo
	err := c.call(ctx, keyID, "flickr.photos.getInfo", "photo_info", func(callCtx context.Context, credentials flickrclient.Credentials) error {
		var callErr error
		result, callErr = c.Client.PhotosGetInfo(callCtx, credentials, photoID, secret)
		return callErr
	})
	return result, err
}

func (c photoFacetClient) PhotosGetExif(ctx context.Context, keyID int64, photoID, secret string) (flickrclient.PhotoExif, error) {
	var result flickrclient.PhotoExif
	err := c.call(ctx, keyID, "flickr.photos.getExif", "photo_exif", func(callCtx context.Context, credentials flickrclient.Credentials) error {
		var callErr error
		result, callErr = c.Client.PhotosGetExif(callCtx, credentials, photoID, secret)
		return callErr
	})
	return result, err
}

func (c photoFacetClient) PhotosGetAllContexts(ctx context.Context, keyID int64, photoID string) (flickrclient.PhotoContexts, error) {
	var result flickrclient.PhotoContexts
	err := c.call(ctx, keyID, "flickr.photos.getAllContexts", "photo_contexts", func(callCtx context.Context, credentials flickrclient.Credentials) error {
		var callErr error
		result, callErr = c.Client.PhotosGetAllContexts(callCtx, credentials, photoID)
		return callErr
	})
	return result, err
}

func (c photoFacetClient) PhotosCommentsGetList(ctx context.Context, keyID int64, photoID string, page int) (flickrclient.Page[flickrclient.PhotoComment], error) {
	var result flickrclient.Page[flickrclient.PhotoComment]
	err := c.call(ctx, keyID, "flickr.photos.comments.getList", "photo_comments", func(callCtx context.Context, credentials flickrclient.Credentials) error {
		var callErr error
		result, callErr = c.Client.PhotosCommentsGetList(callCtx, credentials, photoID, flickrclient.ListOptions{PerPage: 50, Page: page})
		return callErr
	})
	return result, err
}

func (c photoFacetClient) PhotosGetFavorites(ctx context.Context, keyID int64, photoID string, page int) (flickrclient.Page[flickrclient.PhotoFavorite], error) {
	var result flickrclient.Page[flickrclient.PhotoFavorite]
	err := c.call(ctx, keyID, "flickr.photos.getFavorites", "photo_favorites", func(callCtx context.Context, credentials flickrclient.Credentials) error {
		var callErr error
		result, callErr = c.Client.PhotosGetFavorites(callCtx, credentials, photoID, flickrclient.ListOptions{PerPage: 50, Page: page})
		return callErr
	})
	return result, err
}

func (c photoFacetClient) call(ctx context.Context, keyID int64, method, facet string, request func(context.Context, flickrclient.Credentials) error) error {
	if c.Pool == nil || c.Client == nil || c.Quota == nil || ctx == nil || keyID <= 0 {
		return errors.New("crawl: photo facet client dependencies are required")
	}
	var apiKey, status string
	var userID int64
	if err := c.Pool.QueryRow(ctx, `SELECT key_public, status, user_id FROM api_keys WHERE id=$1`, keyID).Scan(&apiKey, &status, &userID); err != nil {
		return fmt.Errorf("crawl: load photo facet key: %w", err)
	}
	if status != "valid" {
		return errors.New("crawl: photo facet key is not valid")
	}
	metadata, _ := flickrclient.CallContextFrom(ctx)
	metadata.KeyID, metadata.UserID, metadata.Lane, metadata.Facet, metadata.NodeID = keyID, &userID, "photo", facet, c.NodeID
	ctx = flickrclient.WithCallContext(ctx, metadata)
	permit, wait, err := c.Quota.Acquire(ctx, keyID, quotadomain.Photo)
	if err != nil {
		return err
	}
	if permit == nil {
		c.Client.RecordQuotaWait(ctx, method)
		return crawlapp.NewQuotaWaitError(flickrclient.ErrQuotaExceeded, wait)
	}
	callErr := request(ctx, flickrclient.Credentials{APIKey: apiKey})
	if doneErr := permit.Done(quotadomain.Meta{Err: callErr}); doneErr != nil {
		if callErr != nil {
			return errors.Join(callErr, fmt.Errorf("crawl: release photo facet quota permit: %w", doneErr))
		}
		return fmt.Errorf("crawl: release photo facet quota permit: %w", doneErr)
	}
	return callErr
}

type photoFacetSettings struct{ settings *settingsapp.Service }

func (s photoFacetSettings) PhotoFacets(ctx context.Context) (scheduleapp.PhotoFacetOptions, error) {
	if s.settings == nil {
		return scheduleapp.PhotoFacetOptions{}, errors.New("scheduler: photo facet settings service is required")
	}
	configured, err := s.settings.Effective(ctx, 0)
	if err != nil {
		return scheduleapp.PhotoFacetOptions{}, err
	}
	return scheduleapp.PhotoFacetOptions{
		Info: configured.PhotoInfoEnabled, Exif: configured.PhotoExifEnabled,
		Contexts: configured.PhotoContextsEnabled, Comments: configured.PhotoCommentsEnabled,
		Favorites: configured.PhotoFavoritesEnabled,
	}, nil
}

func buildCrawlHandler(pool *pgxpool.Pool) (crawlhttp.CrawlHandler, error) {
	return buildCrawlHandlerWithEvents(pool, nil)
}

func buildCrawlHandlerWithEvents(pool *pgxpool.Pool, events downloadEventPublisher) (crawlhttp.CrawlHandler, error) {
	repository, err := crawlpostgres.NewControls(pool)
	if err != nil {
		return crawlhttp.CrawlHandler{}, err
	}
	settingsRepository, err := settingspostgres.New(pool)
	if err != nil {
		return crawlhttp.CrawlHandler{}, err
	}
	depth := crawlapp.Depth{Store: crawlsettings.Depth{Settings: settingsapp.New(settingsRepository)}}
	var controls crawlhttp.ControlService = crawlapp.Controls{Repository: repository, Rescorer: scheduleriver.PersonRescorer{Pool: pool}}
	if events != nil {
		controls = publishingControls{ControlService: controls, Notifier: crawlProgressNotifier{Status: controls, Events: events}}
	}
	return crawlhttp.CrawlHandler{Controls: controls, Depth: depth, ActorID: userIDFromPrincipal}, nil
}
