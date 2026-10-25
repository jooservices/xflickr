package http

import (
	"errors"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

func TestStrictHandlerLeavesAPIKeysToAccounts(t *testing.T) {
	handler := StrictHandler{}
	if _, err := handler.ListKeys(t.Context(), gen.ListKeysRequestObject{}); !errors.Is(err, errAPIKeysServedByAccounts) {
		t.Fatalf("list error = %v", err)
	}
	if _, err := handler.CreateKey(t.Context(), gen.CreateKeyRequestObject{}); !errors.Is(err, errAPIKeysServedByAccounts) {
		t.Fatalf("create error = %v", err)
	}
	if _, err := handler.DeleteKey(t.Context(), gen.DeleteKeyRequestObject{}); !errors.Is(err, errAPIKeysServedByAccounts) {
		t.Fatalf("delete error = %v", err)
	}
	if _, err := handler.GetKey(t.Context(), gen.GetKeyRequestObject{}); !errors.Is(err, errAPIKeysServedByAccounts) {
		t.Fatalf("get error = %v", err)
	}
	if _, err := handler.UpdateKey(t.Context(), gen.UpdateKeyRequestObject{}); !errors.Is(err, errAPIKeysServedByAccounts) {
		t.Fatalf("update error = %v", err)
	}
}

func TestStrictHandlerLeavesFlickrAccountsToAccounts(t *testing.T) {
	handler := StrictHandler{}
	if _, err := handler.ConnectFlickrAccount(t.Context(), gen.ConnectFlickrAccountRequestObject{}); !errors.Is(err, errFlickrAccountsServedByAccounts) {
		t.Fatalf("connect error = %v", err)
	}
	if _, err := handler.FlickrAccountCallback(t.Context(), gen.FlickrAccountCallbackRequestObject{}); !errors.Is(err, errFlickrAccountsServedByAccounts) {
		t.Fatalf("callback error = %v", err)
	}
}

func TestStrictHandlerLeavesPlansToSchedule(t *testing.T) {
	handler := StrictHandler{}
	if _, err := handler.GetAdminPlans(t.Context(), gen.GetAdminPlansRequestObject{}); !errors.Is(err, errPlansServedBySchedule) {
		t.Fatalf("get plans error = %v", err)
	}
	if _, err := handler.UpdateAdminPlans(t.Context(), gen.UpdateAdminPlansRequestObject{}); !errors.Is(err, errPlansServedBySchedule) {
		t.Fatalf("update plans error = %v", err)
	}
	if _, err := handler.UpdateAdminUserPlan(t.Context(), gen.UpdateAdminUserPlanRequestObject{}); !errors.Is(err, errPlansServedBySchedule) {
		t.Fatalf("assign plan error = %v", err)
	}
}
