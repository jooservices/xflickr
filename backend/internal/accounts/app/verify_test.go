package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/queue"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

func TestVerifyClassifiesLoginResultsAndUsesInteractiveQuota(t *testing.T) {
	transient := errors.New("temporary Flickr failure")
	cases := []struct {
		name          string
		accountStatus domain.AccountStatus
		keyStatus     domain.Status
		login         flickrclient.LoginUser
		callErr       error
		wantAccount   domain.AccountStatus
		wantKey       domain.Status
		wantErr       error
		wantCall      bool
		wantRecord    bool
	}{
		{name: "valid login", accountStatus: domain.AccountOK, keyStatus: domain.StatusValid, login: flickrclient.LoginUser{ID: "user-1"}, wantAccount: domain.AccountOK, wantKey: domain.StatusValid, wantCall: true, wantRecord: true},
		{name: "valid login does not clear reconnect requirement", accountStatus: domain.AccountReconnectRequired, keyStatus: domain.StatusValid, login: flickrclient.LoginUser{ID: "user-1"}, wantAccount: domain.AccountReconnectRequired, wantKey: domain.StatusValid, wantCall: true, wantRecord: true},
		{name: "valid login does not undo revoked state", accountStatus: domain.AccountRevoked, keyStatus: domain.StatusValid, login: flickrclient.LoginUser{ID: "user-1"}, wantAccount: domain.AccountRevoked, wantKey: domain.StatusValid, wantCall: true, wantRecord: true},
		{name: "revoked token", accountStatus: domain.AccountOK, keyStatus: domain.StatusValid, callErr: flickrclient.ErrTokenRevoked, wantAccount: domain.AccountRevoked, wantKey: domain.StatusValid, wantCall: true, wantRecord: true},
		{name: "different account", accountStatus: domain.AccountOK, keyStatus: domain.StatusValid, login: flickrclient.LoginUser{ID: "other-user"}, wantAccount: domain.AccountRevoked, wantKey: domain.StatusValid, wantCall: true, wantRecord: true},
		{name: "missing login identity", accountStatus: domain.AccountOK, keyStatus: domain.StatusValid, wantAccount: domain.AccountOK, wantKey: domain.StatusValid, wantErr: ErrInvalidLoginIdentity, wantCall: true},
		{name: "invalid key", accountStatus: domain.AccountOK, keyStatus: domain.StatusValid, callErr: flickrclient.ErrInvalidKey, wantAccount: domain.AccountReconnectRequired, wantKey: domain.StatusInvalid, wantCall: true, wantRecord: true},
		{name: "invalid key preserves revoked account", accountStatus: domain.AccountRevoked, keyStatus: domain.StatusValid, callErr: flickrclient.ErrInvalidKey, wantAccount: domain.AccountRevoked, wantKey: domain.StatusInvalid, wantCall: true, wantRecord: true},
		{name: "stored invalid key", accountStatus: domain.AccountOK, keyStatus: domain.StatusInvalid, wantAccount: domain.AccountReconnectRequired, wantKey: domain.StatusInvalid, wantRecord: true},
		{name: "transient failure", accountStatus: domain.AccountOK, keyStatus: domain.StatusValid, callErr: transient, wantAccount: domain.AccountOK, wantKey: domain.StatusValid, wantErr: transient, wantCall: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			repository := &verificationRepoFake{credentials: verificationCredentials(test.accountStatus, test.keyStatus)}
			checker := &loginCheckerFake{login: test.login, err: test.callErr}
			governor := &verificationGovernorFake{}
			verifier := Verify{Repository: repository, Checker: checker, Cipher: verificationOpener{}, Quota: governor}

			err := verifier.Handle(t.Context(), 42)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Handle() error = %v, want %v", err, test.wantErr)
			}
			if repository.recorded != boolInt(test.wantRecord) || (test.wantRecord && (repository.keyStatus != test.wantKey || repository.accountStatus != test.wantAccount)) {
				t.Fatalf("recorded status = (%s, %s), calls = %d", repository.accountStatus, repository.keyStatus, repository.recorded)
			}
			if checker.calls != boolInt(test.wantCall) {
				t.Fatalf("login calls = %d, want %t", checker.calls, test.wantCall)
			}
			if governor.calls != boolInt(test.wantCall) {
				t.Fatalf("quota calls = %d, want %t", governor.calls, test.wantCall)
			}
			if test.wantCall && !governor.interactive {
				t.Fatal("verification did not request interactive quota")
			}
			if test.wantCall && !errors.Is(governor.permit.doneErr, test.callErr) {
				t.Fatalf("permit outcome = %v, want %v", governor.permit.doneErr, test.callErr)
			}
		})
	}
}

func TestVerifyQuotaWaitDoesNotCallFlickrOrPersist(t *testing.T) {
	wait := 37 * time.Second
	repository := &verificationRepoFake{credentials: verificationCredentials(domain.AccountOK, domain.StatusValid)}
	checker := &loginCheckerFake{}
	governor := &verificationGovernorFake{wait: wait}
	verifier := Verify{Repository: repository, Checker: checker, Cipher: verificationOpener{}, Quota: governor}

	err := verifier.Handle(t.Context(), 42)
	var waiting *QuotaWaitError
	if !errors.As(err, &waiting) || waiting.Wait != wait {
		t.Fatalf("Handle() error = %v, want quota wait %s", err, wait)
	}
	if checker.calls != 0 || repository.recorded != 0 {
		t.Fatalf("Flickr calls = %d, status writes = %d", checker.calls, repository.recorded)
	}
}

func TestVerifyWipesPartialSecretsWhenDecryptionFails(t *testing.T) {
	boom := errors.New("decrypt failed")
	for _, aad := range []string{string(domain.SecretAAD(5)), string(domain.TokenAAD(42)), string(domain.TokenSecretAAD(42))} {
		t.Run(aad, func(t *testing.T) {
			partial := []byte("partial secret")
			repository := &verificationRepoFake{credentials: verificationCredentials(domain.AccountOK, domain.StatusValid)}
			checker := &loginCheckerFake{}
			governor := &verificationGovernorFake{}
			verifier := Verify{Repository: repository, Checker: checker, Cipher: verificationOpener{failAAD: aad, err: boom, partial: partial}, Quota: governor}
			if err := verifier.Handle(t.Context(), 42); !errors.Is(err, boom) {
				t.Fatalf("Handle() decryption error = %v", err)
			}
			if checker.calls != 0 || governor.calls != 0 || repository.recorded != 0 {
				t.Fatalf("Flickr calls=%d quota calls=%d status writes=%d", checker.calls, governor.calls, repository.recorded)
			}
			for _, value := range partial {
				if value != 0 {
					t.Fatal("partial plaintext was not wiped")
				}
			}
		})
	}
}

func TestVerifyPropagatesLoadQuotaAndPersistenceFailures(t *testing.T) {
	boom := errors.New("dependency failed")
	tests := []struct {
		name  string
		setup func(*verificationRepoFake, *verificationGovernorFake, *verificationPermitFake)
	}{
		{name: "load", setup: func(repository *verificationRepoFake, _ *verificationGovernorFake, _ *verificationPermitFake) {
			repository.loadErr = boom
		}},
		{name: "quota acquire", setup: func(_ *verificationRepoFake, governor *verificationGovernorFake, _ *verificationPermitFake) {
			governor.err = boom
		}},
		{name: "quota completion", setup: func(_ *verificationRepoFake, governor *verificationGovernorFake, permit *verificationPermitFake) {
			governor.permit = permit
			permit.err = boom
		}},
		{name: "record status", setup: func(repository *verificationRepoFake, _ *verificationGovernorFake, _ *verificationPermitFake) {
			repository.recordErr = boom
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &verificationRepoFake{credentials: verificationCredentials(domain.AccountOK, domain.StatusValid)}
			checker := &loginCheckerFake{login: flickrclient.LoginUser{ID: "user-1"}}
			governor := &verificationGovernorFake{}
			permit := &verificationPermitFake{}
			test.setup(repository, governor, permit)
			verifier := Verify{Repository: repository, Checker: checker, Cipher: verificationOpener{}, Quota: governor}
			if err := verifier.Handle(t.Context(), 42); !errors.Is(err, boom) {
				t.Fatalf("Handle() error = %v", err)
			}
		})
	}
	if err := (Verify{Repository: &verificationRepoFake{}, Checker: &loginCheckerFake{}, Cipher: verificationOpener{}, Quota: &verificationGovernorFake{}}).Handle(nil, 42); err == nil {
		t.Fatal("Handle() accepted a nil context")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := (Verify{Repository: &verificationRepoFake{}, Checker: &loginCheckerFake{}, Cipher: verificationOpener{}, Quota: &verificationGovernorFake{}}).Handle(ctx, 42); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v", err)
	}
}

func TestVerifyRejectsInvalidInputAndDependencies(t *testing.T) {
	if _, err := (VerifyJob{}).Marshal(); !errors.Is(err, ErrInvalidVerifyJob) {
		t.Fatalf("zero job marshal error = %v", err)
	}
	for _, raw := range []json.RawMessage{nil, json.RawMessage("{}"), json.RawMessage("not-json")} {
		if _, err := ParseVerifyJob(raw); !errors.Is(err, ErrInvalidVerifyJob) {
			t.Errorf("ParseVerifyJob(%q) error = %v", raw, err)
		}
	}
	args, err := (VerifyJob{AccountID: 42}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseVerifyJob(args)
	if err != nil || parsed.AccountID != 42 {
		t.Fatalf("ParseVerifyJob() = %+v, %v", parsed, err)
	}
	if err := (Verify{}).Handle(t.Context(), 42); err == nil {
		t.Fatal("Handle() accepted missing dependencies")
	}
	if err := (Verify{Repository: &verificationRepoFake{}, Checker: &loginCheckerFake{}, Cipher: verificationOpener{}, Quota: &verificationGovernorFake{}}).Handle(t.Context(), 0); !errors.Is(err, ErrInvalidVerifyJob) {
		t.Fatalf("zero account id error = %v", err)
	}
}

func TestVerificationSeederEnqueuesExistingAndNewAccountsUniquely(t *testing.T) {
	jobs := &verificationJobsFake{}
	seeder := VerificationSeeder{Repository: verificationIDsFake{ids: []int64{3, 8}}, Jobs: jobs}
	if err := seeder.Seed(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(jobs.jobs) != 2 {
		t.Fatalf("seeded jobs = %d", len(jobs.jobs))
	}
	if err := seeder.Enqueue(t.Context(), 13); err != nil {
		t.Fatal(err)
	}
	if len(jobs.jobs) != 3 {
		t.Fatalf("enqueued jobs = %d", len(jobs.jobs))
	}
	for _, enqueued := range jobs.jobs {
		if enqueued.job.Kind != VerifyJobKind || enqueued.opts.Queue != VerifyQueue || enqueued.opts.MaxAttempts != 25 || enqueued.opts.Backoff != 5*time.Minute || !enqueued.opts.UniqueByArgs || !enqueued.opts.UniqueByQueue {
			t.Errorf("job options = %+v", enqueued)
		}
		var args VerifyJob
		if err := json.Unmarshal(enqueued.job.Args, &args); err != nil || args.AccountID <= 0 {
			t.Errorf("job args = %s, %v", enqueued.job.Args, err)
		}
	}
}

func verificationCredentials(accountStatus domain.AccountStatus, keyStatus domain.Status) VerificationCredentials {
	return VerificationCredentials{
		Account:     domain.Account{ID: 42, UserID: 7, KeyID: 5, NSID: "user-1", Status: accountStatus},
		Key:         domain.APIKey{ID: 5, UserID: 7, PublicKey: "public-key", Status: keyStatus},
		KeySecret:   crypto.Sealed{Ciphertext: []byte("sealed-key-secret")},
		Token:       crypto.Sealed{Ciphertext: []byte("sealed-token")},
		TokenSecret: crypto.Sealed{Ciphertext: []byte("sealed-token-secret")},
	}
}

type verificationRepoFake struct {
	credentials   VerificationCredentials
	loadErr       error
	recordErr     error
	accountStatus domain.AccountStatus
	keyStatus     domain.Status
	recorded      int
}

func (r *verificationRepoFake) LoadVerification(context.Context, int64) (VerificationCredentials, error) {
	return r.credentials, r.loadErr
}

func (r *verificationRepoFake) RecordVerification(_ context.Context, _ VerificationCredentials, keyStatus domain.Status, accountStatus domain.AccountStatus) error {
	r.recorded++
	r.keyStatus = keyStatus
	r.accountStatus = accountStatus
	return r.recordErr
}

type verificationOpener struct {
	failAAD string
	err     error
	partial []byte
}

func (o verificationOpener) Open(_ crypto.Sealed, aad []byte) ([]byte, error) {
	if string(aad) == o.failAAD {
		return o.partial, o.err
	}
	switch string(aad) {
	case string(domain.SecretAAD(5)):
		return []byte("key-secret"), nil
	case string(domain.TokenAAD(42)):
		return []byte("oauth-token"), nil
	case string(domain.TokenSecretAAD(42)):
		return []byte("oauth-secret"), nil
	default:
		return nil, errors.New("unexpected associated data")
	}
}

type loginCheckerFake struct {
	login flickrclient.LoginUser
	err   error
	calls int
}

func (c *loginCheckerFake) TestLogin(_ context.Context, credentials flickrclient.Credentials) (flickrclient.LoginUser, error) {
	c.calls++
	if credentials.APIKey != "public-key" || credentials.APISecret != "key-secret" || credentials.Token != "oauth-token" || credentials.TokenSecret != "oauth-secret" {
		return flickrclient.LoginUser{}, errors.New("unexpected login credentials")
	}
	return c.login, c.err
}

type verificationGovernorFake struct {
	wait        time.Duration
	err         error
	calls       int
	interactive bool
	permit      *verificationPermitFake
}

func (g *verificationGovernorFake) AcquireInteractive(_ context.Context, keyID int64) (VerificationPermit, time.Duration, error) {
	g.calls++
	g.interactive = true
	if keyID != 5 {
		return nil, 0, errors.New("unexpected key id")
	}
	if g.permit == nil {
		g.permit = &verificationPermitFake{}
	}
	if g.wait > 0 {
		return nil, g.wait, nil
	}
	if g.err != nil {
		return nil, 0, g.err
	}
	return g.permit, 0, nil
}

type verificationPermitFake struct {
	doneErr error
	err     error
}

func (p *verificationPermitFake) Done(callErr error) error {
	p.doneErr = callErr
	return p.err
}

type verificationIDsFake struct{ ids []int64 }

func (r verificationIDsFake) VerificationAccountIDs(context.Context) ([]int64, error) {
	return r.ids, nil
}

type enqueuedVerification struct {
	job  queue.Job
	opts queue.Options
}

type verificationJobsFake struct{ jobs []enqueuedVerification }

func (q *verificationJobsFake) EnqueueStandalone(_ context.Context, job queue.Job, opts queue.Options) error {
	q.jobs = append(q.jobs, enqueuedVerification{job: job, opts: opts})
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
