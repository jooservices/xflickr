package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/platform/crypto"
)

const (
	testPublic = "0123456789abcdef0123456789abcdef"
	testSecret = "super-secret-value"
)

func TestCreateStoresValidKeyAndSealsWithKeyID(t *testing.T) {
	repo := &fakeRepo{}
	sealer := &fakeSealer{}
	probe := &fakeProbe{}
	keys := testKeys(repo, probe, sealer, allowPolicy())
	hour := 100
	created, err := keys.Create(t.Context(), 7, CreateInput{
		Label:          " Personal ",
		Key:            " " + testPublic + " ",
		Secret:         testSecret,
		PerHourLimit:   &hour,
		PerSecondLimit: intPtr(2),
		Burst:          intPtr(3),
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.UserID != 7 || created.Label != "Personal" || created.Status != domain.StatusValid {
		t.Fatalf("created = %+v", created)
	}
	if created.PerHourLimit != 100 || created.PerSecondLimit != 2 || created.Burst != 3 {
		t.Fatalf("limits = %+v", created)
	}
	if probe.calls != 1 || probe.key != testPublic {
		t.Fatalf("probe = %+v", probe)
	}
	if string(sealer.aad) != "api_keys:1" || string(sealer.plaintext) != testSecret {
		t.Fatalf("seal aad=%q plaintext=%q", sealer.aad, sealer.plaintext)
	}
	if repo.created != 1 {
		t.Fatalf("created calls = %d", repo.created)
	}
}

func TestCreateRejectsInvalidKeyWithoutStoring(t *testing.T) {
	repo := &fakeRepo{}
	probe := &fakeProbe{err: ErrKeyInvalid}
	keys := testKeys(repo, probe, &fakeSealer{}, allowPolicy())
	var logs bytes.Buffer
	restore := swapLogger(&logs)
	defer restore()
	_, err := keys.Create(t.Context(), 7, CreateInput{Label: "Personal", Key: testPublic, Secret: testSecret})
	if !errors.Is(err, ErrKeyInvalid) {
		t.Fatalf("error = %v", err)
	}
	if repo.created != 0 || probe.calls != 1 {
		t.Fatalf("created=%d probe=%d", repo.created, probe.calls)
	}
	if strings.Contains(logs.String(), testSecret) || strings.Contains(logs.String(), testPublic) {
		t.Fatalf("log leaked credential: %s", logs.String())
	}
}

func TestCreateDoesNotStoreWhenTheCheckFails(t *testing.T) {
	repo := &fakeRepo{}
	probe := &fakeProbe{err: errors.New("flickr timeout " + testSecret)}
	keys := testKeys(repo, probe, &fakeSealer{}, allowPolicy())
	var logs bytes.Buffer
	restore := swapLogger(&logs)
	defer restore()
	_, err := keys.Create(t.Context(), 7, CreateInput{Label: "Personal", Key: testPublic, Secret: testSecret})
	if !errors.Is(err, ErrKeyCheckFailed) {
		t.Fatalf("error = %v", err)
	}
	if repo.created != 0 {
		t.Fatal("stored a key after a failed check")
	}
	if strings.Contains(logs.String(), testSecret) || strings.Contains(logs.String(), testPublic) || strings.Contains(err.Error(), testSecret) {
		t.Fatalf("credential leaked in log %q or error %q", logs.String(), err.Error())
	}
}

func TestCreateValidatesBeforeCallingFlickr(t *testing.T) {
	repo := &fakeRepo{}
	probe := &fakeProbe{}
	keys := testKeys(repo, probe, &fakeSealer{}, allowPolicy())
	_, err := keys.Create(t.Context(), 7, CreateInput{Label: " ", Key: testPublic, Secret: testSecret})
	if !errors.Is(err, ErrInvalidInput) || probe.calls != 0 || repo.created != 0 {
		t.Fatalf("blank label error=%v probe=%d", err, probe.calls)
	}
	_, err = keys.Create(t.Context(), 7, CreateInput{Label: "Personal", Key: "", Secret: testSecret})
	if !errors.Is(err, ErrKeyInvalid) || probe.calls != 0 {
		t.Fatalf("blank key error=%v probe=%d", err, probe.calls)
	}
	_, err = keys.Create(t.Context(), 7, CreateInput{Label: "Personal", Key: testPublic, Secret: "bad secret"})
	if !errors.Is(err, ErrKeyInvalid) || probe.calls != 0 {
		t.Fatalf("bad secret error=%v probe=%d", err, probe.calls)
	}
	over := 9
	_, err = keys.Create(t.Context(), 7, CreateInput{Label: "Personal", Key: testPublic, Secret: testSecret, PerSecondLimit: &over})
	if !errors.Is(err, domain.ErrLimitExceeded) || probe.calls != 0 || repo.created != 0 {
		t.Fatalf("over cap error=%v probe=%d created=%d", err, probe.calls, repo.created)
	}
	keys.Settings = fakeSettings{err: errors.New("settings down")}
	_, err = keys.Create(t.Context(), 7, CreateInput{Label: "Personal", Key: testPublic, Secret: testSecret})
	if err == nil || probe.calls != 0 || !strings.Contains(err.Error(), "settings down") {
		t.Fatalf("settings error=%v probe=%d", err, probe.calls)
	}
}

func TestCreateUsesAdministratorDefaults(t *testing.T) {
	repo := &fakeRepo{}
	keys := testKeys(repo, &fakeProbe{}, &fakeSealer{}, allowPolicy())
	created, err := keys.Create(t.Context(), 3, CreateInput{Label: "Archive", Key: testPublic, Secret: testSecret})
	if err != nil {
		t.Fatal(err)
	}
	if created.PerHourLimit != domain.FlickrHourlyCeiling || created.PerSecondLimit != 4 || created.Burst != 5 {
		t.Fatalf("defaults = %+v", created)
	}
}

func TestCreateReportsSealAndRepositoryFailures(t *testing.T) {
	repo := &fakeRepo{}
	sealer := &fakeSealer{err: errors.New("seal failed")}
	keys := testKeys(repo, &fakeProbe{}, sealer, allowPolicy())
	_, err := keys.Create(t.Context(), 7, CreateInput{Label: "Personal", Key: testPublic, Secret: testSecret})
	if err == nil || !strings.Contains(err.Error(), "seal failed") || len(repo.keys) != 0 {
		t.Fatalf("seal error=%v stored=%d", err, len(repo.keys))
	}
	sealer.err = nil
	repo.createErr = ErrKeyExists
	_, err = keys.Create(t.Context(), 7, CreateInput{Label: "Personal", Key: testPublic, Secret: testSecret})
	if !errors.Is(err, ErrKeyExists) {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestUpdateChangesLimitsAndSecret(t *testing.T) {
	repo := &fakeRepo{keys: []domain.APIKey{{
		ID: 8, UserID: 7, Label: "Personal", PublicKey: testPublic,
		PerHourLimit: 3600, PerSecondLimit: 4, Burst: 5, Status: domain.StatusValid,
	}}}
	sealer := &fakeSealer{}
	keys := testKeys(repo, &fakeProbe{}, sealer, allowPolicy())
	label := " Archive "
	secret := "rotated-secret-value"
	perSecond := 1
	updated, err := keys.Update(t.Context(), 7, 8, UpdateInput{Label: &label, Secret: &secret, PerSecondLimit: &perSecond})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Label != "Archive" || updated.PerSecondLimit != 1 || updated.PerHourLimit != 3600 || updated.PublicKey != testPublic {
		t.Fatalf("updated = %+v", updated)
	}
	if repo.secretUpdates != 1 || string(sealer.aad) != "api_keys:8" || string(sealer.plaintext) != secret {
		t.Fatalf("secret updates=%d aad=%q", repo.secretUpdates, sealer.aad)
	}
	if repo.probe != 0 {
		t.Fatal("update called Flickr")
	}
}

func TestUpdateKeepsConnectedAccountsWhenSecretIsUnchanged(t *testing.T) {
	repo := &fakeRepo{keys: []domain.APIKey{{
		ID: 8, UserID: 7, Label: "Personal", PublicKey: testPublic,
		PerHourLimit: 100, PerSecondLimit: 4, Burst: 5, Status: domain.StatusInvalid,
	}}}
	keys := testKeys(repo, &fakeProbe{}, &fakeSealer{}, allowPolicy())
	updated, err := keys.Update(t.Context(), 7, 8, UpdateInput{Burst: intPtr(2)})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Burst != 2 || updated.Status != domain.StatusInvalid || repo.secretUpdates != 0 {
		t.Fatalf("updated = %+v secretUpdates=%d", updated, repo.secretUpdates)
	}
}

func TestUpdateRejectsCapsRevokedKeysAndMissingSecrets(t *testing.T) {
	stored := domain.APIKey{
		ID: 8, UserID: 7, Label: "Personal", PublicKey: testPublic,
		PerHourLimit: 100, PerSecondLimit: 4, Burst: 5, Status: domain.StatusValid,
	}
	repo := &fakeRepo{keys: []domain.APIKey{stored}}
	keys := testKeys(repo, &fakeProbe{}, &fakeSealer{}, allowPolicy())
	over := 9
	if _, err := keys.Update(t.Context(), 7, 8, UpdateInput{Burst: &over}); !errors.Is(err, domain.ErrLimitExceeded) || repo.keys[0].Burst != 5 {
		t.Fatalf("burst error = %v key=%+v", err, repo.keys[0])
	}
	blank := " "
	if _, err := keys.Update(t.Context(), 7, 8, UpdateInput{Label: &blank}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("label error = %v", err)
	}
	badSecret := "has space"
	if _, err := keys.Update(t.Context(), 7, 8, UpdateInput{Secret: &badSecret}); !errors.Is(err, ErrKeyInvalid) || repo.secretUpdates != 0 {
		t.Fatalf("secret error = %v updates=%d", err, repo.secretUpdates)
	}
	hour := 0
	if _, err := keys.Update(t.Context(), 7, 8, UpdateInput{PerHourLimit: &hour}); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("hour error = %v", err)
	}
	repo.keys[0].Status = domain.StatusRevoked
	if _, err := keys.Update(t.Context(), 7, 8, UpdateInput{Label: strPtr("Next")}); !errors.Is(err, domain.ErrRevoked) {
		t.Fatalf("revoked error = %v", err)
	}
	repo.keys = nil
	if _, err := keys.Update(t.Context(), 7, 99, UpdateInput{Label: strPtr("Next")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing error = %v", err)
	}
	keys.Settings = fakeSettings{err: errors.New("settings down")}
	if _, err := keys.Update(t.Context(), 7, 8, UpdateInput{}); err == nil || !strings.Contains(err.Error(), "settings down") {
		t.Fatalf("settings error = %v", err)
	}
}

func TestDeleteListAndGet(t *testing.T) {
	repo := &fakeRepo{keys: []domain.APIKey{{ID: 3, UserID: 7, Label: "Personal", PublicKey: testPublic, Status: domain.StatusValid}}}
	keys := testKeys(repo, &fakeProbe{}, &fakeSealer{}, allowPolicy())
	listed, err := keys.List(t.Context(), 7)
	if err != nil || len(listed) != 1 || listed[0].ID != 3 {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	got, err := keys.Get(t.Context(), 7, 3)
	if err != nil || got.PublicKey != testPublic {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := keys.Get(t.Context(), 7, 4); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing get error = %v", err)
	}
	if err := keys.Delete(t.Context(), 7, 3); err != nil {
		t.Fatal(err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != 3 || len(repo.keys) != 0 {
		t.Fatalf("deleted = %+v keys=%d", repo.deleted, len(repo.keys))
	}
	if err := keys.Delete(t.Context(), 7, 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete error = %v", err)
	}
}

func TestServiceRejectsIncompleteSetupAndCanceledContext(t *testing.T) {
	if _, err := (Keys{}).Create(t.Context(), 1, CreateInput{}); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("create error = %v", err)
	}
	keys := testKeys(&fakeRepo{}, &fakeProbe{}, &fakeSealer{}, allowPolicy())
	if _, err := keys.Create(nil, 1, CreateInput{}); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("nil context error = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := keys.List(canceled, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("list error = %v", err)
	}
	if _, err := keys.Get(canceled, 1, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("get error = %v", err)
	}
	if err := keys.Delete(canceled, 1, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("delete error = %v", err)
	}
	if _, err := keys.Update(canceled, 1, 1, UpdateInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("update error = %v", err)
	}
	keys.Repository = nil
	if _, err := keys.Get(t.Context(), 1, 1); err == nil {
		t.Fatal("nil repository succeeded")
	}
}

func testKeys(repo Repository, probe Probe, sealer Sealer, settings Settings) Keys {
	return Keys{Repository: repo, Probe: probe, Sealer: sealer, Settings: settings}
}

func allowPolicy() fakeSettings {
	policy, err := domain.PolicyFromSettings(0, 4, 5)
	if err != nil {
		panic(err)
	}
	return fakeSettings{policy: policy}
}

func intPtr(value int) *int { return &value }

func strPtr(value string) *string { return &value }

func swapLogger(buf *bytes.Buffer) func() {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	return func() { slog.SetDefault(previous) }
}

type fakeProbe struct {
	calls int
	key   string
	err   error
}

func (p *fakeProbe) Echo(_ context.Context, key string) error {
	p.calls++
	p.key = key
	return p.err
}

type fakeSealer struct {
	plaintext []byte
	aad       []byte
	err       error
}

func (s *fakeSealer) Seal(plaintext, aad []byte) (crypto.Sealed, error) {
	s.plaintext = append([]byte(nil), plaintext...)
	s.aad = append([]byte(nil), aad...)
	if s.err != nil {
		return crypto.Sealed{}, s.err
	}
	return dummySealed(), nil
}

func dummySealed() crypto.Sealed {
	return crypto.Sealed{
		KeyID:      "test",
		Nonce:      bytes.Repeat([]byte{1}, 12),
		Ciphertext: bytes.Repeat([]byte{2}, 16),
		WrappedDEK: bytes.Repeat([]byte{3}, 60),
	}
}

type fakeSettings struct {
	policy domain.Policy
	err    error
}

func (s fakeSettings) Policy(context.Context, int64) (domain.Policy, error) {
	if s.err != nil {
		return domain.Policy{}, s.err
	}
	return s.policy, nil
}

type fakeRepo struct {
	keys          []domain.APIKey
	created       int
	secretUpdates int
	deleted       []int64
	createErr     error
	probe         int
	nextID        int64
}

func (r *fakeRepo) Create(_ context.Context, key domain.APIKey, seal func(int64) (crypto.Sealed, error)) (domain.APIKey, error) {
	r.created++
	if r.createErr != nil {
		return domain.APIKey{}, r.createErr
	}
	r.nextID++
	if _, err := seal(r.nextID); err != nil {
		return domain.APIKey{}, err
	}
	key.ID = r.nextID
	key.CreatedAt = time.Unix(1_700_000_000, 0).UTC()
	key.Status = domain.StatusValid
	r.keys = append(r.keys, key)
	return key, nil
}

func (r *fakeRepo) Update(_ context.Context, userID, id int64, apply func(domain.APIKey) (domain.APIKey, *crypto.Sealed, error)) (domain.APIKey, error) {
	for index, key := range r.keys {
		if key.ID != id || key.UserID != userID {
			continue
		}
		next, sealed, err := apply(key)
		if err != nil {
			return domain.APIKey{}, err
		}
		if sealed != nil {
			r.secretUpdates++
		}
		next.ID = key.ID
		next.UserID = key.UserID
		next.PublicKey = key.PublicKey
		next.Status = key.Status
		next.CreatedAt = key.CreatedAt
		r.keys[index] = next
		return next, nil
	}
	return domain.APIKey{}, ErrNotFound
}

func (r *fakeRepo) Delete(_ context.Context, userID, id int64) error {
	for index, key := range r.keys {
		if key.ID == id && key.UserID == userID {
			r.deleted = append(r.deleted, id)
			r.keys = append(r.keys[:index], r.keys[index+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (r *fakeRepo) List(_ context.Context, userID int64) ([]domain.APIKey, error) {
	var keys []domain.APIKey
	for _, key := range r.keys {
		if key.UserID == userID {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (r *fakeRepo) Get(_ context.Context, userID, id int64) (domain.APIKey, error) {
	for _, key := range r.keys {
		if key.ID == id && key.UserID == userID {
			return key, nil
		}
	}
	return domain.APIKey{}, ErrNotFound
}
