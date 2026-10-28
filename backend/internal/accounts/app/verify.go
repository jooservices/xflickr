package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/queue"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

const (
	// VerifyJobKind is the durable job type for checking a connected Flickr account.
	VerifyJobKind = "accounts.verify"
	// VerifyQueue is the worker queue that pays for account verification.
	VerifyQueue = "accounts"
	// VerifyInterval is the interval between successful token checks.
	VerifyInterval = 24 * time.Hour
)

var (
	ErrInvalidVerifyJob     = errors.New("accounts: invalid verification job")
	ErrVerificationChanged  = errors.New("accounts: verification credentials changed during check")
	ErrInvalidLoginIdentity = errors.New("accounts: Flickr login returned no user identifier")
)

// VerifyJob identifies one account to verify. It contains no credentials.
type VerifyJob struct {
	AccountID int64 `json:"account_id"`
}

func (j VerifyJob) Marshal() (json.RawMessage, error) {
	if j.AccountID <= 0 {
		return nil, ErrInvalidVerifyJob
	}
	encoded, err := json.Marshal(j)
	if err != nil {
		return nil, fmt.Errorf("accounts: encode verification job: %w", err)
	}
	return encoded, nil
}

func ParseVerifyJob(raw json.RawMessage) (VerifyJob, error) {
	var job VerifyJob
	if len(raw) == 0 || !json.Valid(raw) {
		return VerifyJob{}, ErrInvalidVerifyJob
	}
	if err := json.Unmarshal(raw, &job); err != nil || job.AccountID <= 0 {
		return VerifyJob{}, ErrInvalidVerifyJob
	}
	return job, nil
}

// VerificationCredentials contains the sealed credentials needed for one login check.
type VerificationCredentials struct {
	Account     domain.Account
	Key         domain.APIKey
	KeySecret   crypto.Sealed
	Token       crypto.Sealed
	TokenSecret crypto.Sealed
}

// VerificationRepository loads sealed account credentials and records the latest status.
type VerificationRepository interface {
	LoadVerification(context.Context, int64) (VerificationCredentials, error)
	RecordVerification(context.Context, VerificationCredentials, domain.Status, domain.AccountStatus) error
}

// LoginChecker verifies that an account's OAuth token still authenticates.
type LoginChecker interface {
	TestLogin(context.Context, flickrclient.Credentials) (flickrclient.LoginUser, error)
}

// VerificationPermit records the result of an account health check.
type VerificationPermit interface {
	Done(error) error
}

// VerificationQuota acquires capacity from the reserved interactive lane.
type VerificationQuota interface {
	AcquireInteractive(context.Context, int64) (VerificationPermit, time.Duration, error)
}

// Verify checks one account using interactive quota and persists only a classified status.
type Verify struct {
	Repository VerificationRepository
	Checker    LoginChecker
	Cipher     Opener
	Quota      VerificationQuota
}

// QuotaWaitError means the interactive lane did not grant a permit yet.
type QuotaWaitError struct{ Wait time.Duration }

func (e *QuotaWaitError) Error() string {
	return "accounts: verification waiting for interactive quota"
}
func (e *QuotaWaitError) Unwrap() error { return flickrclient.ErrQuotaExceeded }

func (v Verify) Handle(ctx context.Context, accountID int64) error {
	if v.Repository == nil || v.Checker == nil || v.Cipher == nil || v.Quota == nil {
		return errors.New("accounts: verification dependencies are required")
	}
	if ctx == nil {
		return errors.New("accounts: verification context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if accountID <= 0 {
		return ErrInvalidVerifyJob
	}
	credentials, err := v.Repository.LoadVerification(ctx, accountID)
	if err != nil {
		return err
	}
	keyStatus := credentials.Key.Status
	if keyStatus != domain.StatusValid {
		return v.recordUnusableKey(ctx, credentials)
	}
	keySecret, token, tokenSecret, err := openVerificationSecrets(v.Cipher, credentials)
	if err != nil {
		return err
	}
	defer wipeBytes(keySecret)
	defer wipeBytes(token)
	defer wipeBytes(tokenSecret)
	permit, wait, err := v.Quota.AcquireInteractive(ctx, credentials.Key.ID)
	if err != nil {
		return err
	}
	if permit == nil {
		return &QuotaWaitError{Wait: wait}
	}
	login, callErr := v.Checker.TestLogin(ctx, flickrclient.Credentials{
		APIKey: credentials.Key.PublicKey, APISecret: string(keySecret), Token: string(token), TokenSecret: string(tokenSecret),
	})
	if doneErr := permit.Done(callErr); doneErr != nil {
		return fmt.Errorf("accounts: record verification quota outcome: %w", doneErr)
	}
	keyStatus, accountStatus, err := classifyVerification(credentials, login, callErr)
	if err != nil {
		return err
	}
	return v.Repository.RecordVerification(ctx, credentials, keyStatus, accountStatus)
}

func (v Verify) recordUnusableKey(ctx context.Context, credentials VerificationCredentials) error {
	accountStatus := credentials.Account.Status
	if accountStatus != domain.AccountRevoked {
		accountStatus = domain.AccountReconnectRequired
	}
	return v.Repository.RecordVerification(ctx, credentials, credentials.Key.Status, accountStatus)
}

func openVerificationSecrets(cipher Opener, credentials VerificationCredentials) (keySecret, token, tokenSecret []byte, err error) {
	keySecret, err = cipher.Open(credentials.KeySecret, domain.SecretAAD(credentials.Key.ID))
	if err != nil {
		wipeBytes(keySecret)
		return nil, nil, nil, fmt.Errorf("accounts: open verification key secret: %w", err)
	}
	token, err = cipher.Open(credentials.Token, domain.TokenAAD(credentials.Account.ID))
	if err != nil {
		wipeBytes(keySecret)
		wipeBytes(token)
		return nil, nil, nil, fmt.Errorf("accounts: open verification token: %w", err)
	}
	tokenSecret, err = cipher.Open(credentials.TokenSecret, domain.TokenSecretAAD(credentials.Account.ID))
	if err != nil {
		wipeBytes(keySecret)
		wipeBytes(token)
		wipeBytes(tokenSecret)
		return nil, nil, nil, fmt.Errorf("accounts: open verification token secret: %w", err)
	}
	return keySecret, token, tokenSecret, nil
}

func classifyVerification(credentials VerificationCredentials, login flickrclient.LoginUser, callErr error) (domain.Status, domain.AccountStatus, error) {
	keyStatus := credentials.Key.Status
	accountStatus := credentials.Account.Status
	switch {
	case errors.Is(callErr, flickrclient.ErrInvalidKey):
		keyStatus = domain.StatusInvalid
		if accountStatus != domain.AccountRevoked {
			accountStatus = domain.AccountReconnectRequired
		}
	case errors.Is(callErr, flickrclient.ErrTokenRevoked):
		accountStatus = domain.AccountRevoked
	case callErr != nil:
		return keyStatus, accountStatus, callErr
	case strings.TrimSpace(login.ID) == "":
		return keyStatus, accountStatus, ErrInvalidLoginIdentity
	case strings.TrimSpace(login.ID) != credentials.Account.NSID:
		accountStatus = domain.AccountRevoked
	default:
		// A health check cannot clear a reconnect or revoked state.
	}
	return keyStatus, accountStatus, nil
}

// VerificationSeeder ensures every existing and newly connected account has one recurring job.
type VerificationSeeder struct {
	Repository interface {
		VerificationAccountIDs(context.Context) ([]int64, error)
	}
	Jobs interface {
		EnqueueStandalone(context.Context, queue.Job, queue.Options) error
	}
}

func (s VerificationSeeder) Seed(ctx context.Context) error {
	if s.Repository == nil || s.Jobs == nil {
		return errors.New("accounts: verification seeder dependencies are required")
	}
	if ctx == nil {
		return errors.New("accounts: verification seeder context is required")
	}
	ids, err := s.Repository.VerificationAccountIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.Enqueue(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (s VerificationSeeder) Enqueue(ctx context.Context, accountID int64) error {
	if s.Jobs == nil || ctx == nil || accountID <= 0 {
		return ErrInvalidVerifyJob
	}
	args, err := (VerifyJob{AccountID: accountID}).Marshal()
	if err != nil {
		return err
	}
	return s.Jobs.EnqueueStandalone(ctx, queue.Job{Kind: VerifyJobKind, Args: args}, queue.Options{
		Queue: VerifyQueue, MaxAttempts: 25, Backoff: 5 * time.Minute,
		UniqueByArgs: true, UniqueByQueue: true,
	})
}
