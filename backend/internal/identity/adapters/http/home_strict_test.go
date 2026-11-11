package http

import (
	"errors"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

func TestStrictHandlerDelegatesHomeToHomeModule(t *testing.T) {
	_, err := (StrictHandler{}).GetMeHome(t.Context(), gen.GetMeHomeRequestObject{})
	if !errors.Is(err, errHomeServedByHome) {
		t.Fatalf("GetMeHome() error = %v, want home delegation error", err)
	}
}
