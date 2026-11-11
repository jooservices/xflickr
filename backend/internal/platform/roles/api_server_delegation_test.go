package roles

import (
	"testing"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/httpx"
)

// Unauthenticated, empty requests must be answered by the delegated handlers
// without panicking; this pins that apiServer forwards these admin operations.
func TestAPIServerForwardsAdminOperations(t *testing.T) {
	server := apiServer{}
	if response, err := server.GetAdminFlag(t.Context(), gen.GetAdminFlagRequestObject{Name: "flag"}); err == nil && response == nil {
		t.Fatal("GetAdminFlag returned neither a response nor an error")
	}
	if response, err := server.GetAdminFlags(t.Context(), gen.GetAdminFlagsRequestObject{}); err == nil && response == nil {
		t.Fatal("GetAdminFlags returned neither a response nor an error")
	}
	if response, err := server.UpdateAdminFlags(t.Context(), gen.UpdateAdminFlagsRequestObject{}); err == nil && response == nil {
		t.Fatal("UpdateAdminFlags returned neither a response nor an error")
	}
	if response, err := server.CreateAdminNode(t.Context(), gen.CreateAdminNodeRequestObject{}); err == nil && response == nil {
		t.Fatal("CreateAdminNode returned neither a response nor an error")
	}
}

func TestHealthLockPoolRequiresPool(t *testing.T) {
	if _, err := (healthLockPool{}).Acquire(t.Context()); err == nil {
		t.Fatal("Acquire() accepted a missing pool")
	}
}

func TestReadinessFromContextHandlesMissingValues(t *testing.T) {
	//nolint:staticcheck // a nil context is part of the contract under test
	if readinessFromContext(nil) != nil {
		t.Fatal("nil context produced a readiness value")
	}
	if readinessFromContext(t.Context()) != nil {
		t.Fatal("context without readiness produced a value")
	}
	readiness := &httpx.Readiness{}
	if readinessFromContext(withReadiness(t.Context(), readiness)) != readiness {
		t.Fatal("stored readiness was not returned")
	}
}
