package http

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/accounts/app"
	"github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

const (
	publicKey = "0123456789abcdef0123456789abcdef"
	secretKey = "super-secret-value"
)

func TestKeyEndpointsRequireAUser(t *testing.T) {
	handler := KeyHandler{Keys: &scriptedKeys{}, Principal: func(context.Context) (int64, bool) { return 0, false }}
	ctx := t.Context()
	if response, err := handler.ListKeys(ctx, gen.ListKeysRequestObject{}); err != nil || !codeIs(t, response, "unauthenticated") {
		t.Fatalf("list = %T %v", response, err)
	}
	if response, err := handler.CreateKey(ctx, gen.CreateKeyRequestObject{Body: createBody("Personal")}); err != nil || !codeIs(t, response, "unauthenticated") {
		t.Fatalf("create = %T %v", response, err)
	}
	if response, err := handler.GetKey(ctx, gen.GetKeyRequestObject{ID: 1}); err != nil || !codeIs(t, response, "unauthenticated") {
		t.Fatalf("get = %T %v", response, err)
	}
	if response, err := handler.UpdateKey(ctx, gen.UpdateKeyRequestObject{ID: 1, Body: &gen.UpdateKeyRequest{}}); err != nil || !codeIs(t, response, "unauthenticated") {
		t.Fatalf("update = %T %v", response, err)
	}
	if response, err := handler.DeleteKey(ctx, gen.DeleteKeyRequestObject{ID: 1}); err != nil || !codeIs(t, response, "unauthenticated") {
		t.Fatalf("delete = %T %v", response, err)
	}

	missing := KeyHandler{Keys: &scriptedKeys{}}
	if response, err := missing.ListKeys(nil, gen.ListKeysRequestObject{}); err != nil || !codeIs(t, response, "unauthenticated") {
		t.Fatalf("nil context = %T %v", response, err)
	}
	if response, err := missing.CreateKey(t.Context(), gen.CreateKeyRequestObject{Body: createBody("Personal")}); err != nil || !codeIs(t, response, "unauthenticated") {
		t.Fatalf("nil principal = %T %v", response, err)
	}
}

func TestKeyEndpointsRejectAMissingService(t *testing.T) {
	handler := signedIn(nil)
	if _, err := handler.ListKeys(t.Context(), gen.ListKeysRequestObject{}); err == nil || strings.Contains(err.Error(), secretKey) {
		t.Fatalf("list error = %v", err)
	}
	if _, err := handler.CreateKey(t.Context(), gen.CreateKeyRequestObject{Body: createBody("Personal")}); err == nil {
		t.Fatal("create accepted a missing service")
	}
	if _, err := handler.GetKey(t.Context(), gen.GetKeyRequestObject{ID: 1}); err == nil {
		t.Fatal("get accepted a missing service")
	}
	if _, err := handler.UpdateKey(t.Context(), gen.UpdateKeyRequestObject{ID: 1, Body: &gen.UpdateKeyRequest{}}); err == nil {
		t.Fatal("update accepted a missing service")
	}
	if _, err := handler.DeleteKey(t.Context(), gen.DeleteKeyRequestObject{ID: 1}); err == nil {
		t.Fatal("delete accepted a missing service")
	}
}

func TestCreateKeyMasksThePublicKeyAndOmitsTheSecret(t *testing.T) {
	service := &scriptedKeys{created: storedKey(domain.StatusValid)}
	handler := signedIn(service)
	secret := secretKey
	hour, second, burst := 100, 2, 3
	body := createBody("Personal")
	body.Secret = &secret
	body.PerHourLimit = &hour
	body.PerSecondLimit = &second
	body.Burst = &burst
	response, err := handler.CreateKey(t.Context(), gen.CreateKeyRequestObject{Body: body})
	if err != nil {
		t.Fatal(err)
	}
	created, ok := response.(gen.CreateKey201JSONResponse)
	if !ok {
		t.Fatalf("response = %T", response)
	}
	assertMasked(t, gen.APIKey(created))
	if service.userID != 9 || service.create.Label != "Personal" || service.create.Key != publicKey || service.create.Secret != secretKey {
		t.Fatalf("create input = %+v user %d", service.create, service.userID)
	}
	if service.create.PerHourLimit == nil || *service.create.PerHourLimit != 100 || *service.create.PerSecondLimit != 2 || *service.create.Burst != 3 {
		t.Fatalf("limits were not forwarded: %+v", service.create)
	}
}

func TestCreateKeyMapsValidationAndFlickrFailures(t *testing.T) {
	secret := secretKey
	for _, tc := range []struct {
		name string
		body *gen.CreateKeyRequest
		err  error
		code string
	}{
		{name: "missing body", code: "invalid_input"},
		{name: "blank label", body: createBody("Personal"), err: app.ErrInvalidInput, code: "invalid_input"},
		{name: "rejected key", body: &gen.CreateKeyRequest{Label: "Personal", Key: publicKey, Secret: &secret}, err: app.ErrKeyInvalid, code: "key_invalid"},
		{name: "over cap", body: createBody("Personal"), err: domain.ErrLimitExceeded, code: "limit_exceeded"},
		{name: "duplicate", body: createBody("Personal"), err: app.ErrKeyExists, code: "key_exists"},
		{name: "flickr down", body: createBody("Personal"), err: app.ErrKeyCheckFailed, code: "key_check_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := signedIn(&scriptedKeys{err: tc.err})
			response, err := handler.CreateKey(t.Context(), gen.CreateKeyRequestObject{Body: tc.body})
			if err != nil || !codeIs(t, response, tc.code) {
				t.Fatalf("response = %#v err = %v", response, err)
			}
			if text := encoded(t, response); strings.Contains(text, secretKey) || strings.Contains(text, publicKey) {
				t.Fatalf("error body leaked key material: %s", text)
			}
		})
	}
}

func TestCreateKeyReturnsInfrastructureErrorsWithoutTheSecret(t *testing.T) {
	secret := secretKey
	handler := signedIn(&scriptedKeys{err: errors.New("database unavailable")})
	body := createBody("Personal")
	body.Secret = &secret
	response, err := handler.CreateKey(t.Context(), gen.CreateKeyRequestObject{Body: body})
	if response != nil || err == nil || err.Error() != "database unavailable" || strings.Contains(err.Error(), secretKey) {
		t.Fatalf("response = %#v err = %v", response, err)
	}

	omitted := &scriptedKeys{err: app.ErrKeyInvalid}
	omittedResponse, err := signedIn(omitted).CreateKey(t.Context(), gen.CreateKeyRequestObject{Body: &gen.CreateKeyRequest{Label: "Personal", Key: publicKey}})
	if err != nil || !codeIs(t, omittedResponse, "key_invalid") || omitted.create.Secret != "" {
		t.Fatalf("omitted secret response = %#v err = %v secret = %q", omittedResponse, err, omitted.create.Secret)
	}
	if _, err := signedIn(&scriptedKeys{err: app.ErrNotFound}).CreateKey(t.Context(), gen.CreateKeyRequestObject{Body: body}); err == nil {
		t.Fatal("create mapped a status it does not declare")
	}
}

func TestUpdateKeyChangesLabelAndKeepsTheSecretOutOfTheResponse(t *testing.T) {
	service := &scriptedKeys{updated: storedKey(domain.StatusInvalid)}
	handler := signedIn(service)
	label := "Archive"
	secret := "rotated-secret-value"
	hour := 80
	response, err := handler.UpdateKey(t.Context(), gen.UpdateKeyRequestObject{ID: 4, Body: &gen.UpdateKeyRequest{Label: &label, Secret: &secret, PerHourLimit: &hour}})
	if err != nil {
		t.Fatal(err)
	}
	updated, ok := response.(gen.UpdateKey200JSONResponse)
	if !ok || updated.Label != "Archive" || updated.Status != gen.Invalid || updated.PerHourLimit != 3600 {
		t.Fatalf("response = %#v", response)
	}
	assertMasked(t, gen.APIKey(updated))
	if service.updateID != 4 || service.update.Label == nil || *service.update.Label != "Archive" || service.update.Secret == nil || *service.update.Secret != secret {
		t.Fatalf("update input = %+v id %d", service.update, service.updateID)
	}
	if strings.Contains(encoded(t, response), secret) {
		t.Fatal("update response included the secret")
	}
}

func TestUpdateKeyMapsStatusAndLimitErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		body *gen.UpdateKeyRequest
		err  error
		code string
	}{
		{name: "missing body", code: "invalid_input"},
		{name: "label", body: &gen.UpdateKeyRequest{}, err: app.ErrInvalidInput, code: "invalid_input"},
		{name: "secret", body: &gen.UpdateKeyRequest{}, err: app.ErrKeyInvalid, code: "key_invalid"},
		{name: "cap", body: &gen.UpdateKeyRequest{}, err: domain.ErrLimitExceeded, code: "limit_exceeded"},
		{name: "revoked", body: &gen.UpdateKeyRequest{}, err: domain.ErrRevoked, code: "key_revoked"},
		{name: "missing", body: &gen.UpdateKeyRequest{}, err: app.ErrNotFound, code: "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := signedIn(&scriptedKeys{err: tc.err})
			response, err := handler.UpdateKey(t.Context(), gen.UpdateKeyRequestObject{ID: 4, Body: tc.body})
			if err != nil || !codeIs(t, response, tc.code) {
				t.Fatalf("response = %#v err = %v", response, err)
			}
		})
	}
	handler := signedIn(&scriptedKeys{err: app.ErrKeyCheckFailed})
	if _, err := handler.UpdateKey(t.Context(), gen.UpdateKeyRequestObject{ID: 4, Body: &gen.UpdateKeyRequest{}}); err == nil || strings.Contains(err.Error(), secretKey) {
		t.Fatalf("unexpected update error = %v", err)
	}
}

func TestGetListAndDelete(t *testing.T) {
	revoked := storedKey(domain.StatusRevoked)
	revoked.ID = 8
	revoked.PublicKey = "short"
	service := &scriptedKeys{got: storedKey(domain.StatusValid), listed: []domain.APIKey{storedKey(domain.StatusValid), revoked}}
	handler := signedIn(service)
	got, err := handler.GetKey(t.Context(), gen.GetKeyRequestObject{ID: 4})
	if err != nil {
		t.Fatal(err)
	}
	body, ok := got.(gen.GetKey200JSONResponse)
	if !ok || body.ID != 4 || service.getID != 4 {
		t.Fatalf("get = %#v id %d", got, service.getID)
	}
	assertMasked(t, gen.APIKey(body))

	listed, err := handler.ListKeys(t.Context(), gen.ListKeysRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	keys, ok := listed.(gen.ListKeys200JSONResponse)
	if !ok || len(keys) != 2 || keys[1].Status != gen.Revoked || keys[1].KeyPublic != "•••••" {
		t.Fatalf("list = %#v", listed)
	}
	if strings.Contains(encoded(t, listed), publicKey) {
		t.Fatal("list included the full public key")
	}

	emptyService := &scriptedKeys{}
	empty, err := signedIn(emptyService).ListKeys(t.Context(), gen.ListKeysRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if keys, ok := empty.(gen.ListKeys200JSONResponse); !ok || keys == nil || len(keys) != 0 {
		t.Fatalf("empty list = %#v", empty)
	}

	deleted, err := handler.DeleteKey(t.Context(), gen.DeleteKeyRequestObject{ID: 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := deleted.(gen.DeleteKey204Response); !ok || service.deleteID != 4 {
		t.Fatalf("delete = %T id %d", deleted, service.deleteID)
	}
}

func TestGetAndDeleteMapMissingKeys(t *testing.T) {
	handler := signedIn(&scriptedKeys{err: app.ErrNotFound})
	if response, err := handler.GetKey(t.Context(), gen.GetKeyRequestObject{ID: 4}); err != nil || !codeIs(t, response, "not_found") {
		t.Fatalf("get = %#v %v", response, err)
	}
	if response, err := handler.DeleteKey(t.Context(), gen.DeleteKeyRequestObject{ID: 4}); err != nil || !codeIs(t, response, "not_found") {
		t.Fatalf("delete = %#v %v", response, err)
	}
	broken := signedIn(&scriptedKeys{err: errors.New("database unavailable")})
	if _, err := broken.GetKey(t.Context(), gen.GetKeyRequestObject{ID: 4}); err == nil || strings.Contains(err.Error(), secretKey) {
		t.Fatalf("get error = %v", err)
	}
	if _, err := broken.DeleteKey(t.Context(), gen.DeleteKeyRequestObject{ID: 4}); err == nil {
		t.Fatal("delete swallowed an infrastructure error")
	}
	if _, err := broken.ListKeys(t.Context(), gen.ListKeysRequestObject{}); err == nil {
		t.Fatal("list swallowed an infrastructure error")
	}
	if response, err := signedIn(&scriptedKeys{err: domain.ErrRevoked}).DeleteKey(t.Context(), gen.DeleteKeyRequestObject{ID: 4}); response != nil || err == nil {
		t.Fatalf("delete revoked = %#v %v", response, err)
	}
}

func signedIn(service KeyService) KeyHandler {
	return KeyHandler{Keys: service, Principal: func(context.Context) (int64, bool) { return 9, true }}
}

func storedKey(status domain.Status) domain.APIKey {
	return domain.APIKey{
		ID: 4, UserID: 9, Label: "Personal", PublicKey: publicKey,
		PerHourLimit: 3600, PerSecondLimit: 4, Burst: 5, Status: status,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
}

func createBody(label string) *gen.CreateKeyRequest {
	secret := secretKey
	return &gen.CreateKeyRequest{Label: label, Key: publicKey, Secret: &secret}
}

func assertMasked(t *testing.T, key gen.APIKey) {
	t.Helper()
	if key.KeyPublic == publicKey || !strings.HasPrefix(key.KeyPublic, publicKey[:domain.PublicKeyVisiblePrefix]) {
		t.Fatalf("key_public = %q", key.KeyPublic)
	}
	raw, err := json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, secretKey) || strings.Contains(text, publicKey) || strings.Contains(text, `"secret"`) {
		t.Fatalf("response leaked key material: %s", text)
	}
}

func codeIs(t *testing.T, response any, code string) bool {
	t.Helper()
	return strings.Contains(encoded(t, response), `"code":"`+code+`"`)
}

func encoded(t *testing.T, response any) string {
	t.Helper()
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

type scriptedKeys struct {
	created domain.APIKey
	updated domain.APIKey
	got     domain.APIKey
	listed  []domain.APIKey
	err     error

	userID   int64
	create   app.CreateInput
	update   app.UpdateInput
	updateID int64
	getID    int64
	deleteID int64
}

func (s *scriptedKeys) Create(_ context.Context, userID int64, input app.CreateInput) (domain.APIKey, error) {
	s.userID = userID
	s.create = input
	if s.err != nil {
		return domain.APIKey{}, s.err
	}
	created := s.created
	created.UserID = userID
	created.Label = input.Label
	return created, nil
}

func (s *scriptedKeys) Update(_ context.Context, userID, id int64, input app.UpdateInput) (domain.APIKey, error) {
	s.userID = userID
	s.updateID = id
	s.update = input
	if s.err != nil {
		return domain.APIKey{}, s.err
	}
	updated := s.updated
	if input.Label != nil {
		updated.Label = *input.Label
	}
	return updated, nil
}

func (s *scriptedKeys) Delete(_ context.Context, userID, id int64) error {
	s.userID = userID
	s.deleteID = id
	return s.err
}

func (s *scriptedKeys) List(_ context.Context, userID int64) ([]domain.APIKey, error) {
	s.userID = userID
	if s.err != nil {
		return nil, s.err
	}
	return s.listed, nil
}

func (s *scriptedKeys) Get(_ context.Context, userID, id int64) (domain.APIKey, error) {
	s.userID = userID
	s.getID = id
	if s.err != nil {
		return domain.APIKey{}, s.err
	}
	return s.got, nil
}
