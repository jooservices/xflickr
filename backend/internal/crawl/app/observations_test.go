package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/crawl/domain"
)

type resolvingFlickr struct {
	*fakeFlickr
	err error
}

func (f resolvingFlickr) Resolve(_ context.Context, r Request) (Request, error) {
	r.KeyID = 10
	r.AccountID = 20
	return r, f.err
}

type scopedFakeCatalog struct {
	CatalogWriter
	user, account int64
	err           error
}

func (w *scopedFakeCatalog) ForObserver(_ context.Context, user, account int64) (CatalogWriter, error) {
	w.user = user
	w.account = account
	return w.CatalogWriter, w.err
}

type scopedFakeUnit struct {
	tx      *fakeTx
	catalog *scopedFakeCatalog
}

func (u scopedFakeUnit) Do(_ context.Context, fn func(Writers) error) error {
	return fn(Writers{Work: u.tx, Catalog: u.catalog, Interests: u.tx, Observations: u.tx})
}

type scopedGovernor struct{ key int64 }

func (g *scopedGovernor) Acquire(_ context.Context, key int64, _ domain.Lane) (Permit, time.Duration, error) {
	g.key = key
	return &fakePermit{}, 0, nil
}

func TestEngineBindsActualAccountBeforeQuotaAndWrites(t *testing.T) {
	for _, fail := range []string{"", "resolve", "writer"} {
		t.Run(fail, func(t *testing.T) {
			rig := newRig(t)
			resolver := resolvingFlickr{fakeFlickr: rig.flickr}
			writer := &scopedFakeCatalog{CatalogWriter: rig.tx}
			if fail == "resolve" {
				resolver.err = errors.New("no account")
			}
			if fail == "writer" {
				writer.err = errors.New("invalid attribution")
			}
			rig.engine.Flickr = resolver
			rig.engine.Units = scopedFakeUnit{tx: rig.tx, catalog: writer}
			governor := &scopedGovernor{}
			rig.engine.Governor = governor
			err := rig.engine.Handle(t.Context(), rig.store.item.ID)
			if fail == "resolve" {
				if err != nil || governor.key != 0 || len(rig.flickr.requests) != 0 || rig.store.deferral.ConsumeAttempt {
					t.Fatalf("resolution failure spent quota: %v", err)
				}
				return
			}
			if (err != nil) != (fail == "writer") {
				t.Fatalf("Handle=%v", err)
			}
			if governor.key != 10 || len(rig.flickr.requests) != 1 || rig.flickr.requests[0].AccountID != 20 || writer.user != rig.store.item.UserID || writer.account != 20 {
				t.Fatalf("governor=%+v requests=%+v writer=%+v", governor, rig.flickr.requests, writer)
			}
			if fail == "writer" && len(rig.tx.log) != 0 {
				t.Fatal("invalid attribution wrote catalog")
			}
		})
	}
}
