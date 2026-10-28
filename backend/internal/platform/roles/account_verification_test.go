package roles

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"
	"github.com/riverqueue/river/rivertype"

	accountsapp "github.com/jooservices/xflickr/internal/accounts/app"
	"github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/queue"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

func TestNewAccountVerifierWiring(t *testing.T) {
	if _, _, err := newAccountVerifier(nil, nil, config.Config{}); err == nil {
		t.Fatal("account verifier accepted a nil database")
	}
	client := redisv9.NewClient(&redisv9.Options{Addr: "127.0.0.1:1"})
	defer client.Close()
	pool := &pgxpool.Pool{}
	if _, _, err := newAccountVerifier(pool, nil, config.Config{}); err == nil {
		t.Fatal("account verifier accepted a nil Redis client")
	}
	if _, _, err := newAccountVerifier(pool, client, config.Config{}); err == nil {
		t.Fatal("account verifier accepted a missing keyring")
	}
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	cfg := config.Config{Crypto: config.CryptoConfig{MasterKeys: "test:" + key, ActiveMasterKeyID: "test"}}
	repository, verifier, err := newAccountVerifier(pool, client, cfg)
	if err != nil || repository == nil || verifier.Repository == nil || verifier.Checker == nil || verifier.Cipher == nil || verifier.Quota == nil {
		t.Fatalf("repository=%v verifier=%+v error=%v", repository != nil, verifier, err)
	}
}

func TestRegisterAccountVerificationSchedulesDailyAndWaitsForQuota(t *testing.T) {
	adapter, err := queue.NewRiver(&pgxpool.Pool{}, map[string]int{accountsapp.VerifyQueue: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adapter.Stop(context.Background()) })

	checker := &verificationCheckerForRole{}
	governor := &verificationGovernorForRole{}
	repository := verificationRepositoryForRole{}
	verifier := accountsapp.Verify{
		Repository: &repository,
		Checker:    checker,
		Cipher:     verificationCipherForRole{},
		Quota:      interactiveAccountQuota{Governor: governor},
	}
	if err := registerAccountVerification(adapter, verifier); err != nil {
		t.Fatal(err)
	}
	if err := registerAccountVerification(adapter, verifier); err == nil {
		t.Fatal("duplicate account verification handler was accepted")
	}
	jobArgs, err := (accountsapp.VerifyJob{AccountID: 42}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	job := queue.Job{Kind: accountsapp.VerifyJobKind, Args: jobArgs}
	var snooze *rivertype.JobSnoozeError
	handler := accountVerificationHandler(verifier)
	if err := handler(t.Context(), job); !errors.As(err, &snooze) || snooze.Duration != accountsapp.VerifyInterval {
		t.Fatalf("successful check reschedule = %v", err)
	}
	if checker.calls != 1 || governor.class != quotadomain.Interactive {
		t.Fatalf("checker calls=%d quota class=%q", checker.calls, governor.class)
	}

	governor.wait = 75 * time.Second
	if err := handler(t.Context(), job); !errors.As(err, &snooze) || snooze.Duration != 75*time.Second {
		t.Fatalf("quota wait reschedule = %v", err)
	}
	if checker.calls != 1 {
		t.Fatalf("checker called while quota unavailable: %d", checker.calls)
	}

	verificationFailure := errors.New("temporary verification failure")
	repository.err = verificationFailure
	if err := handler(t.Context(), job); !errors.Is(err, verificationFailure) {
		t.Fatalf("verification failure = %v", err)
	}

	if err := handler(t.Context(), queue.Job{Kind: accountsapp.VerifyJobKind, Args: json.RawMessage(`{}`)}); !errors.Is(err, accountsapp.ErrInvalidVerifyJob) {
		t.Fatalf("malformed job error = %v", err)
	}
}

func TestStartAccountVerificationRegistersAndSeedsJobs(t *testing.T) {
	adapter, err := queue.NewRiver(&pgxpool.Pool{}, map[string]int{accountsapp.VerifyQueue: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adapter.Stop(context.Background()) })
	repository := &verificationRepositoryForRole{ids: []int64{4, 9}}
	jobs := &verificationJobsForRole{}
	seeder := accountsapp.VerificationSeeder{Repository: repository, Jobs: jobs}
	if err := startAccountVerification(t.Context(), adapter, accountsapp.Verify{}, seeder); err != nil {
		t.Fatal(err)
	}
	if jobs.enqueued != 2 {
		t.Fatalf("seeded verification jobs = %d", jobs.enqueued)
	}
	if err := startAccountVerification(t.Context(), adapter, accountsapp.Verify{}, seeder); err == nil {
		t.Fatal("duplicate verification registration was accepted")
	}
	if jobs.enqueued != 2 {
		t.Fatalf("duplicate registration seeded additional jobs: %d", jobs.enqueued)
	}

	queryErr := errors.New("account scan unavailable")
	failedAdapter, err := queue.NewRiver(&pgxpool.Pool{}, map[string]int{accountsapp.VerifyQueue: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = failedAdapter.Stop(context.Background()) })
	repository.idsErr = queryErr
	if err := startAccountVerification(t.Context(), failedAdapter, accountsapp.Verify{}, seeder); !errors.Is(err, queryErr) {
		t.Fatalf("seeder repository error = %v", err)
	}
	if failedAdapter.Register(accountsapp.VerifyJobKind, func(context.Context, queue.Job) error { return nil }) == nil {
		t.Fatal("failed seeding should still keep the worker handler registered")
	}
}

type verificationRepositoryForRole struct {
	err    error
	ids    []int64
	idsErr error
}

func (repository *verificationRepositoryForRole) LoadVerification(context.Context, int64) (accountsapp.VerificationCredentials, error) {
	return accountsapp.VerificationCredentials{
		Account:   domain.Account{ID: 42, UserID: 7, KeyID: 5, NSID: "user-1", Status: domain.AccountOK},
		Key:       domain.APIKey{ID: 5, UserID: 7, PublicKey: "public-key", Status: domain.StatusValid},
		KeySecret: crypto.Sealed{}, Token: crypto.Sealed{}, TokenSecret: crypto.Sealed{},
	}, repository.err
}

func (*verificationRepositoryForRole) RecordVerification(context.Context, accountsapp.VerificationCredentials, domain.Status, domain.AccountStatus) error {
	return nil
}

func (repository *verificationRepositoryForRole) VerificationAccountIDs(context.Context) ([]int64, error) {
	return repository.ids, repository.idsErr
}

type verificationJobsForRole struct{ enqueued int }

func (jobs *verificationJobsForRole) EnqueueStandalone(context.Context, queue.Job, queue.Options) error {
	jobs.enqueued++
	return nil
}

type verificationCheckerForRole struct{ calls int }

func (checker *verificationCheckerForRole) TestLogin(context.Context, flickrclient.Credentials) (flickrclient.LoginUser, error) {
	checker.calls++
	return flickrclient.LoginUser{ID: "user-1"}, nil
}

type verificationCipherForRole struct{}

func (verificationCipherForRole) Open(crypto.Sealed, []byte) ([]byte, error) {
	return []byte("secret"), nil
}

type verificationGovernorForRole struct {
	wait  time.Duration
	class quotadomain.Class
}

func (governor *verificationGovernorForRole) Acquire(_ context.Context, _ int64, class quotadomain.Class) (quotadomain.Permit, time.Duration, error) {
	governor.class = class
	if governor.wait > 0 {
		return nil, governor.wait, nil
	}
	return verificationPermitForRole{}, 0, nil
}

type verificationPermitForRole struct{}

func (verificationPermitForRole) Done(quotadomain.Meta) error { return nil }
