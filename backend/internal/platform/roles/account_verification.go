package roles

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"

	accountsflickr "github.com/jooservices/xflickr/internal/accounts/adapters/flickr"
	accountspostgres "github.com/jooservices/xflickr/internal/accounts/adapters/postgres"
	accountsapp "github.com/jooservices/xflickr/internal/accounts/app"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/queue"
	quotaPostgres "github.com/jooservices/xflickr/internal/quota/adapters/postgres"
	quotaRedis "github.com/jooservices/xflickr/internal/quota/adapters/redis"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
)

func newAccountVerifier(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config) (*accountspostgres.Repository, accountsapp.Verify, error) {
	if pool == nil {
		return nil, accountsapp.Verify{}, errors.New("accounts: PostgreSQL pool is required for verification")
	}
	if redisClient == nil {
		return nil, accountsapp.Verify{}, errors.New("accounts: Redis client is required for verification")
	}
	repository, err := accountspostgres.New(pool)
	if err != nil {
		return nil, accountsapp.Verify{}, err
	}
	cipher, err := keyringFromConfig(cfg)
	if err != nil {
		return nil, accountsapp.Verify{}, err
	}
	quota := quotaRedis.Governor{Client: redisClient, Prefix: cfg.Redis.Prefix, Budgets: quotaPostgres.Budgets{Pool: pool}, Recorder: quotaPostgres.CallLog{Pool: pool}}
	verifier := accountsapp.Verify{
		Repository: repository,
		Checker:    accountsflickr.NewLoginProbe(flickrRESTURL(cfg), nil),
		Cipher:     cipher,
		Quota:      interactiveAccountQuota{Governor: quota},
	}
	return repository, verifier, nil
}

type interactiveAccountQuota struct{ Governor quotadomain.Governor }

func (q interactiveAccountQuota) AcquireInteractive(ctx context.Context, keyID int64) (accountsapp.VerificationPermit, time.Duration, error) {
	if q.Governor == nil {
		return nil, 0, errors.New("accounts: interactive quota governor is required")
	}
	permit, wait, err := q.Governor.Acquire(ctx, keyID, quotadomain.Interactive)
	if err != nil || permit == nil {
		return nil, wait, err
	}
	return interactiveAccountPermit{Permit: permit}, wait, nil
}

type interactiveAccountPermit struct{ Permit quotadomain.Permit }

func (p interactiveAccountPermit) Done(err error) error {
	return p.Permit.Done(quotadomain.Meta{Err: err})
}

// registerAccountVerification keeps token checks on the dedicated accounts
// queue and reschedules completed checks once per day without using retries.
func registerAccountVerification(queueAdapter *queue.River, verifier accountsapp.Verify) error {
	return queueAdapter.RegisterWithOptions(accountsapp.VerifyJobKind, accountVerificationHandler(verifier), queue.Options{Queue: accountsapp.VerifyQueue, MaxAttempts: 25, Backoff: 5 * time.Minute})
}

func startAccountVerification(ctx context.Context, queueAdapter *queue.River, verifier accountsapp.Verify, seeder accountsapp.VerificationSeeder) error {
	if err := registerAccountVerification(queueAdapter, verifier); err != nil {
		return err
	}
	if err := seeder.Seed(ctx); err != nil {
		return fmt.Errorf("crawl: seed account verification jobs: %w", err)
	}
	return nil
}

func accountVerificationHandler(verifier accountsapp.Verify) queue.Handler {
	return func(ctx context.Context, job queue.Job) error {
		args, err := accountsapp.ParseVerifyJob(job.Args)
		if err != nil {
			return err
		}
		err = verifier.Handle(ctx, args.AccountID)
		var waiting *accountsapp.QuotaWaitError
		if errors.As(err, &waiting) {
			return queue.Snooze(waiting.Wait)
		}
		if err != nil {
			return err
		}
		return queue.SnoozeFor(accountsapp.VerifyInterval)
	}
}
