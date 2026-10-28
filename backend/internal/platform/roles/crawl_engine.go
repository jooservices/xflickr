package roles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"

	accountsdomain "github.com/jooservices/xflickr/internal/accounts/domain"
	catalogpostgres "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	crawlpostgres "github.com/jooservices/xflickr/internal/crawl/adapters/postgres"
	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	crawldomain "github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/internal/crawl/facets"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/obs"
	"github.com/jooservices/xflickr/internal/platform/queue"
	quotaPostgres "github.com/jooservices/xflickr/internal/quota/adapters/postgres"
	quotaRedis "github.com/jooservices/xflickr/internal/quota/adapters/redis"
	quotaapp "github.com/jooservices/xflickr/internal/quota/app"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
	scheduleriver "github.com/jooservices/xflickr/internal/schedule/adapters/river"
	settingspostgres "github.com/jooservices/xflickr/internal/settings/adapters/postgres"
	settingsapp "github.com/jooservices/xflickr/internal/settings/app"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

// crawlWorkers is how many sync-queue jobs one crawl process runs at once.
const crawlWorkers = 4

// registerCrawlJobs registers one River handler per crawl facet. Each job is
// one page of one work item, run by the crawl engine.
func registerCrawlJobs(queueAdapter *queue.River, engine *crawlapp.Engine) error {
	for _, kind := range engine.Registry.Names() {
		err := queueAdapter.RegisterWithOptions(kind, func(ctx context.Context, job queue.Job) error {
			id, err := crawlapp.ParseJobArgs(job.Args)
			if err != nil {
				return err
			}
			return engine.Handle(ctx, id)
		}, queue.Options{Queue: scheduleriver.PhotoQueue})
		if err != nil {
			return err
		}
	}
	return nil
}

// newCrawlEngine wires the crawl engine to PostgreSQL, the quota governor and
// the Flickr client, with every facet registered.
func newCrawlEngine(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config, flickr *flickrclient.Client) (*crawlapp.Engine, error) {
	ring, err := keyringFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	store, err := crawlpostgres.NewEngine(pool, func(conn crawlpostgres.Conn) crawlapp.CatalogWriter {
		repository, repositoryErr := catalogpostgres.New(conn)
		if repositoryErr != nil {
			return nil
		}
		return repository
	})
	if err != nil {
		return nil, err
	}
	settingsRepository, err := settingspostgres.New(pool)
	if err != nil {
		return nil, err
	}
	registry := crawlapp.NewRegistry()
	for _, facet := range []crawldomain.Facet{crawlapp.ChangeCheck{}, crawlapp.ContactsPhotos{}, facets.Photos{}, facets.Favorites{}, facets.Albums{}, facets.Galleries{}, facets.Groups{}} {
		if err := registry.Register(facet); err != nil {
			return nil, err
		}
	}
	governor := quotaRedis.Governor{Client: redisClient, Prefix: cfg.Redis.Prefix, Budgets: quotaPostgres.Budgets{Pool: pool}, Recorder: quotaPostgres.CallLog{Pool: pool}}
	return &crawlapp.Engine{
		Registry: registry,
		Store:    store,
		Units:    store,
		Keys:     crawlKeys{pool: quotaapp.KeyPool{Interests: personInterests{pool: pool}, Keys: usableKeys{pool: pool}, Quota: governor}},
		Governor: crawlGovernor{governor: governor},
		Flickr:   &crawlFlickr{pool: pool, client: flickr, ring: ring},
		Settings: settingsapp.New(settingsRepository),
		Logger:   slog.Default(),
	}, nil
}

// laneClass maps a crawl lane to the quota class that pays for it.
func laneClass(lane crawldomain.Lane) quotadomain.Class {
	if lane == crawldomain.LanePhoto {
		return quotadomain.Photo
	}
	return quotadomain.Person
}

// crawlKeys adapts the quota key pool to the crawl engine.
type crawlKeys struct{ pool quotaapp.KeyPool }

func (k crawlKeys) Choose(ctx context.Context, person string, lane crawldomain.Lane) (int64, error) {
	id, err := k.pool.Choose(ctx, person, laneClass(lane))
	if errors.Is(err, quotaapp.ErrNoKey) {
		return 0, crawlapp.ErrNoKey
	}
	return int64(id), err
}

// personInterests lists the users who may crawl a person: those with a crawl
// interest in them and the user whose own account the person is.
type personInterests struct {
	pool interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	}
}

func (p personInterests) InterestedUsers(ctx context.Context, nsid string) ([]int64, error) {
	rows, err := p.pool.Query(ctx, `SELECT user_id FROM crawl_interests WHERE person_nsid=$1 UNION SELECT user_id FROM flickr_accounts WHERE nsid=$1 AND status='ok' ORDER BY 1`, nsid)
	if err != nil {
		return nil, fmt.Errorf("crawl: list interested users: %w", err)
	}
	defer rows.Close()
	var users []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("crawl: scan interested user: %w", err)
		}
		users = append(users, id)
	}
	return users, rows.Err()
}

// usableKeys lists the keys of a user that can sign calls: valid keys that
// have a connected account in good standing.
type usableKeys struct {
	pool interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	}
}

func (u usableKeys) KeysOf(ctx context.Context, userID int64) ([]quotaapp.PoolKey, error) {
	rows, err := u.pool.Query(ctx, `SELECT k.id FROM api_keys k WHERE k.user_id=$1 AND k.status='valid' AND EXISTS (SELECT 1 FROM flickr_accounts a WHERE a.key_id=k.id AND a.user_id=k.user_id AND a.status='ok') ORDER BY k.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("crawl: list usable keys: %w", err)
	}
	defer rows.Close()
	var keys []quotaapp.PoolKey
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("crawl: scan usable key: %w", err)
		}
		keys = append(keys, quotaapp.PoolKey{ID: quotaapp.KeyID(id), Valid: true})
	}
	return keys, rows.Err()
}

// crawlGovernor adapts the quota governor to the crawl engine.
type crawlGovernor struct{ governor quotadomain.Governor }

type crawlPermit struct{ permit quotadomain.Permit }

func (p crawlPermit) Done(callErr error) error { return p.permit.Done(quotadomain.Meta{Err: callErr}) }

func (g crawlGovernor) Acquire(ctx context.Context, keyID int64, lane crawldomain.Lane) (crawlapp.Permit, time.Duration, error) {
	permit, wait, err := g.governor.Acquire(ctx, keyID, laneClass(lane))
	if err != nil || permit == nil {
		return nil, wait, err
	}
	return crawlPermit{permit: permit}, wait, nil
}

// crawlFlickr signs a Flickr call with the key and the account token of the
// user. Secrets are opened per call and never kept.
type crawlFlickr struct {
	pool interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}
	client *flickrclient.Client
	ring   *crypto.Keyring
}

func (f *crawlFlickr) Call(ctx context.Context, request crawlapp.Request) (json.RawMessage, error) {
	fields := make(map[string]any, 3)
	if request.KeyID > 0 {
		fields["key_id"] = request.KeyID
	}
	if request.UserID > 0 {
		fields["user_id"] = request.UserID
	}
	if request.Lane != "" {
		fields["lane"] = string(request.Lane)
	}
	ctx = obs.WithFields(ctx, fields)
	creds, err := f.credentials(ctx, request)
	if err != nil {
		return nil, err
	}
	payload, _, err := f.client.Call(ctx, creds, request.Method, request.Params)
	return payload, err
}

func (f *crawlFlickr) credentials(ctx context.Context, request crawlapp.Request) (flickrclient.Credentials, error) {
	var public string
	var sealedSecret crypto.Sealed
	if err := f.pool.QueryRow(ctx, `SELECT key_public, secret_sealed FROM api_keys WHERE id=$1 AND status='valid'`, request.KeyID).Scan(&public, &sealedSecret); err != nil {
		return flickrclient.Credentials{}, fmt.Errorf("crawl: load key %d: %w", request.KeyID, err)
	}
	secret, err := f.ring.Open(sealedSecret, accountsdomain.SecretAAD(request.KeyID))
	if err != nil {
		return flickrclient.Credentials{}, fmt.Errorf("crawl: open key %d secret: %w", request.KeyID, err)
	}
	creds := flickrclient.Credentials{APIKey: public, APISecret: string(secret)}
	if request.UserID <= 0 {
		return creds, nil
	}
	var accountID int64
	var sealedToken, sealedTokenSecret crypto.Sealed
	if err := f.pool.QueryRow(ctx, `SELECT id, token_sealed, secret_sealed FROM flickr_accounts WHERE user_id=$1 AND key_id=$2 AND status='ok' ORDER BY id LIMIT 1`, request.UserID, request.KeyID).Scan(&accountID, &sealedToken, &sealedTokenSecret); err != nil {
		return flickrclient.Credentials{}, fmt.Errorf("crawl: load account of user %d for key %d: %w", request.UserID, request.KeyID, err)
	}
	token, err := f.ring.Open(sealedToken, accountsdomain.TokenAAD(accountID))
	if err != nil {
		return flickrclient.Credentials{}, fmt.Errorf("crawl: open account %d token: %w", accountID, err)
	}
	tokenSecret, err := f.ring.Open(sealedTokenSecret, accountsdomain.TokenSecretAAD(accountID))
	if err != nil {
		return flickrclient.Credentials{}, fmt.Errorf("crawl: open account %d token secret: %w", accountID, err)
	}
	creds.Token, creds.TokenSecret = string(token), string(tokenSecret)
	return creds, nil
}
