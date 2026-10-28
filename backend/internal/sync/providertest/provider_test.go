package providertest_test

import (
	"context"
	"testing"

	"github.com/jooservices/xflickr/internal/sync/adapters/fake"
	"github.com/jooservices/xflickr/internal/sync/app"
	"github.com/jooservices/xflickr/internal/sync/providertest"
)

func TestRun(t *testing.T) {
	providertest.Run(t, func(_ testing.TB, scenario providertest.Scenario) app.StorageProvider {
		return fake.New(scenario.UploadErrors...)
	})
}

func TestRunWithoutDeleteCapability(t *testing.T) {
	providertest.Run(t, func(_ testing.TB, scenario providertest.Scenario) app.StorageProvider {
		return noDeleteProvider{StorageProvider: fake.New(scenario.UploadErrors...)}
	})
}

type noDeleteProvider struct{ app.StorageProvider }

func (p noDeleteProvider) Capabilities() app.Capabilities {
	capabilities := p.StorageProvider.Capabilities()
	capabilities.SupportsDelete = false
	return capabilities
}

func (noDeleteProvider) Delete(context.Context, app.RemoteItemID) error {
	return app.ErrUnsupported
}
