package roles

import (
	"context"
	"fmt"

	catalogpostgres "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres/observations"
	catalogdomain "github.com/jooservices/xflickr/internal/catalog/domain"
	crawlpostgres "github.com/jooservices/xflickr/internal/crawl/adapters/postgres"
	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db"
)

// observedCatalog binds only people/photo upserts; the remaining catalog methods
// keep using the same transaction through the embedded repository.
type observedCatalog struct {
	*catalogpostgres.Repository
	conn crawlpostgres.Conn
}

type scopedCatalog struct {
	*catalogpostgres.Repository
	*observations.Writer
}

func (s scopedCatalog) UpsertPeople(ctx context.Context, values []catalogdomain.Person) error {
	return s.Writer.UpsertPeople(ctx, values)
}

func (s scopedCatalog) UpsertPhotos(ctx context.Context, values []catalogdomain.Photo) error {
	return s.Writer.UpsertPhotos(ctx, values)
}

func (w *observedCatalog) ForObserver(_ context.Context, userID, accountID int64) (crawlapp.CatalogWriter, error) {
	writer, err := observations.New(w.conn, userID, accountID)
	if err != nil {
		return nil, err
	}
	return scopedCatalog{Repository: w.Repository, Writer: writer}, nil
}

// Resolve chooses only the work user's account, even when the shared key pool
// suggested a different user's key. Pinning its ID keeps signing and recording
// consistent if account eligibility changes while the HTTP call runs.
func (f *crawlFlickr) Resolve(ctx context.Context, request crawlapp.Request) (crawlapp.Request, error) {
	if request.UserID <= 0 {
		return request, fmt.Errorf("crawl: observation user is required")
	}
	err := f.pool.QueryRow(ctx, `SELECT a.key_id, a.id FROM flickr_accounts a
 JOIN api_keys k ON k.id=a.key_id AND k.user_id=a.user_id
 WHERE a.user_id=$1 AND a.status='ok' AND k.status='valid'
 ORDER BY (a.key_id=$2) DESC, a.id LIMIT 1`, request.UserID, request.KeyID).Scan(&request.KeyID, &request.AccountID)
	if err != nil {
		return request, fmt.Errorf("crawl: resolve work user's account: %w", err)
	}
	return request, nil
}

// ObservationBackfillOptions identifies the operator-attested legacy observer.
type ObservationBackfillOptions = observations.BackfillOptions

// ObservationBackfillReport includes counts even when attribution is refused.
type ObservationBackfillReport = observations.BackfillReport

// BackfillObservations runs the bounded catalog backfill against the configured
// database. It makes no Flickr calls and never starts application roles.
func BackfillObservations(ctx context.Context, options ObservationBackfillOptions) (ObservationBackfillReport, error) {
	cfg, err := config.LoadDatabase()
	if err != nil {
		return ObservationBackfillReport{}, err
	}
	pool, err := db.NewPool(ctx, cfg)
	if err != nil {
		return ObservationBackfillReport{}, err
	}
	defer pool.Close()
	return observations.Backfill(ctx, pool, options)
}
