package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/crawl/domain"
)

var (
	// ErrNoKey means no eligible API key can serve the subject right now.
	ErrNoKey = errors.New("crawl: no eligible key")
	// ErrItemNotFound means the work item does not exist (it was deleted).
	ErrItemNotFound = errors.New("crawl: work item not found")
	// ErrEngineIncomplete means a required dependency is missing.
	ErrEngineIncomplete = errors.New("crawl: engine dependencies are required")
	// ErrCredentialDecryption means a persisted credential could not be opened.
	ErrCredentialDecryption = errors.New("crawl: stored credential cannot be decrypted")
)

// KeyChooser picks the API key that pays for a call about a person.
// Implementations return ErrNoKey when none is eligible.
type KeyChooser interface {
	Choose(ctx context.Context, personNSID string, lane domain.Lane) (int64, error)
}

// Permit is a granted quota permit. Done reports the call outcome, nil on success.
type Permit interface {
	Done(callErr error) error
}

// Governor hands out quota permits. A nil permit with a positive wait means
// no permit is available before that duration passes.
type Governor interface {
	Acquire(ctx context.Context, keyID int64, lane domain.Lane) (Permit, time.Duration, error)
}

// Request is one Flickr call on behalf of a work item.
type Request struct {
	KeyID  int64
	UserID int64
	Lane   domain.Lane
	Method string
	Params map[string]string
}

// Flickr performs a call with the credentials of the key and user.
type Flickr interface {
	Call(ctx context.Context, request Request) (json.RawMessage, error)
}

// NodeHealth receives reports that this node's IP is blocked (JOOXF-13).
type NodeHealth interface {
	ReportIPBlocked(ctx context.Context, keyID int64, cause error) error
}

// Deferral holds an item back until NotBefore.
type Deferral struct {
	NotBefore      time.Time
	ConsumeAttempt bool
	Reason         string
}

// StopRequest stops every active item of a user or key whose credential
// failed, and notifies the owner.
type StopRequest struct {
	ItemID int64
	UserID int64
	KeyID  int64
	Reason StopReason
}

// Store changes a work item outside a page commit.
type Store interface {
	Load(ctx context.Context, id int64) (domain.Item, error)
	// Start moves a ready item to running; it reports false when the item is
	// not runnable (finished, or already reclaimed).
	Start(ctx context.Context, id int64) (bool, error)
	Defer(ctx context.Context, id int64, deferral Deferral) error
	Finish(ctx context.Context, id int64) error
	Fail(ctx context.Context, id int64, reason string) error
	Stop(ctx context.Context, request StopRequest) error
}

// NewWork is a work item created while committing a page.
type NewWork struct {
	Kind    string
	Subject string
	UserID  int64
	Depth   int
	Cursor  domain.Cursor
	Reopen  bool
}

// WorkWriter changes work items inside the commit transaction.
type WorkWriter interface {
	// Lock reads the item and holds it until the transaction ends.
	Lock(ctx context.Context, id int64) (domain.Item, error)
	Enqueue(ctx context.Context, work NewWork) error
	Advance(ctx context.Context, id int64, cursor domain.Cursor) error
	Finish(ctx context.Context, id int64) error
}

// CatalogWriter upserts catalog records inside the commit transaction.
type CatalogWriter interface {
	UpsertPeople(ctx context.Context, people []catalog.Person) error
	UpsertPhotos(ctx context.Context, photos []catalog.Photo) error
	UpsertFavorites(ctx context.Context, favorites []catalog.Favorite) error
	UpsertAlbums(ctx context.Context, albums []catalog.Album) error
	UpsertGalleries(ctx context.Context, galleries []catalog.Gallery) error
	UpsertGroups(ctx context.Context, groups []catalog.Group) error
	UpsertPersonGroups(ctx context.Context, groups []catalog.PersonGroup) error
}

// InterestWriter records interests inside the commit transaction.
type InterestWriter interface {
	Add(ctx context.Context, interest domain.Interest) error
}

// ObservationWriter records the observations of one page. Recording the same
// page twice stores nothing new.
type ObservationWriter interface {
	Record(ctx context.Context, itemID int64, page int, observations []domain.Observation) error
}

// ReconcileWriter applies the diff from a completed photos-and-favorites scan
// atomically with its final page.
type ReconcileWriter interface {
	Complete(ctx context.Context, userID int64, person string, scannedAt, completedAt time.Time) error
}

// Writers is everything the commit transaction writes through.
type Writers struct {
	Work         WorkWriter
	Catalog      CatalogWriter
	Interests    InterestWriter
	Observations ObservationWriter
	Reconcile    ReconcileWriter
}

// UnitOfWork runs fn in one atomic transaction.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(Writers) error) error
}

// ProgressNotifier is told that a user's crawl state changed after a page was
// processed, so live progress can be pushed to that user.
type ProgressNotifier interface {
	CrawlChanged(ctx context.Context, userID int64) error
}

// Engine runs work items for every registered facet: one task is one API page.
type Engine struct {
	Registry *Registry
	Store    Store
	Units    UnitOfWork
	Keys     KeyChooser
	Governor Governor
	Flickr   Flickr
	Health   NodeHealth
	Progress ProgressNotifier
	Settings EffectiveSettings
	Timing   Timing
	Logger   *slog.Logger
	Now      func() time.Time
}

// ParseJobArgs reads the work item id from the scheduler's job arguments.
func ParseJobArgs(args []byte) (int64, error) {
	var payload struct {
		WorkItemID int64 `json:"work_item_id"`
	}
	if err := json.Unmarshal(args, &payload); err != nil || payload.WorkItemID <= 0 {
		return 0, errors.New("crawl: job arguments need a work_item_id")
	}
	return payload.WorkItemID, nil
}

// Handle processes the next page of one work item. Failures that belong to
// the item (quota, missing subject, bad credential, blocked IP, transient)
// are recorded on the item and return nil; an error means the page could not
// be committed and the job should run again, which is safe.
func (e *Engine) Handle(ctx context.Context, id int64) error {
	if err := e.ready(ctx); err != nil {
		return err
	}
	item, err := e.Store.Load(ctx, id)
	if errors.Is(err, ErrItemNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if item.State == domain.StateDone || item.State == domain.StateFailed {
		return nil
	}
	facet, ok := e.Registry.Get(item.Kind)
	if !ok {
		return e.Store.Fail(ctx, id, "unknown facet "+strconv.Quote(item.Kind))
	}
	started, err := e.Store.Start(ctx, id)
	if err != nil || !started {
		return err
	}
	if err := e.run(ctx, item, facet); err != nil {
		return err
	}
	e.notifyProgress(ctx, item.UserID)
	return nil
}

// notifyProgress reports a crawl state change; a failed notification never
// fails the page, which is already committed.
func (e *Engine) notifyProgress(ctx context.Context, userID int64) {
	if e.Progress == nil || userID <= 0 {
		return
	}
	if err := e.Progress.CrawlChanged(ctx, userID); err != nil {
		e.logger().WarnContext(ctx, "crawl progress notification failed", "user_id", userID, "error", err)
	}
}

func (e *Engine) run(ctx context.Context, item domain.Item, facet domain.Facet) error {
	keyID, err := e.Keys.Choose(ctx, item.Subject, facet.Lane())
	if errors.Is(err, ErrNoKey) {
		return e.hold(ctx, item, e.Timing.withDefaults().QuotaWait, "no eligible key")
	}
	if err != nil {
		return err
	}
	permit, wait, err := e.Governor.Acquire(ctx, keyID, facet.Lane())
	if err != nil {
		return e.hold(ctx, item, e.Timing.withDefaults().QuotaWait, "quota governor: "+err.Error())
	}
	if permit == nil {
		return e.hold(ctx, item, wait, "waiting for quota")
	}
	payload, callErr := e.Flickr.Call(ctx, Request{KeyID: keyID, UserID: item.UserID, Lane: facet.Lane(), Method: item.Cursor.Method, Params: pageParams(item.Cursor)})
	if doneErr := permit.Done(callErr); doneErr != nil {
		e.logger().WarnContext(ctx, "crawl permit release failed", "work_item_id", item.ID, "error", doneErr)
	}
	if callErr != nil {
		return e.fail(ctx, item, keyID, callErr)
	}
	result, err := facet.Handle(ctx, domain.Page{Item: item, Number: item.Cursor.Page, Payload: payload})
	if err != nil {
		return e.fail(ctx, item, keyID, err)
	}
	return e.commit(ctx, item, result)
}

// pageParams adds the page number to the item's request parameters.
func pageParams(cursor domain.Cursor) map[string]string {
	params := make(map[string]string, len(cursor.Params)+1)
	for key, value := range cursor.Params {
		params[key] = value
	}
	params["page"] = strconv.Itoa(cursor.Page)
	return params
}

// hold waits without consuming an attempt.
func (e *Engine) hold(ctx context.Context, item domain.Item, wait time.Duration, reason string) error {
	return e.Store.Defer(ctx, item.ID, Deferral{NotBefore: e.now().Add(wait), Reason: reason})
}

// fail applies the outcome of a failed call or page.
func (e *Engine) fail(ctx context.Context, item domain.Item, keyID int64, cause error) error {
	timing := e.Timing.withDefaults()
	var decryptionFailure interface{ CredentialDecryptionFailure() }
	if errors.Is(cause, ErrCredentialDecryption) || errors.As(cause, &decryptionFailure) {
		return e.hold(ctx, item, timing.CredentialWait, CredentialDecryptionReason)
	}
	switch Classify(cause) {
	case OutcomeWaitQuota:
		return e.hold(ctx, item, timing.QuotaWait, cause.Error())
	case OutcomeDone:
		return e.Store.Finish(ctx, item.ID)
	case OutcomeUnavailable:
		e.logger().WarnContext(ctx, "crawl resource unavailable", "work_item_id", item.ID, "kind", item.Kind, "error", cause)
		return e.Store.Fail(ctx, item.ID, UnavailableReason(cause))
	case OutcomeStop:
		return e.Store.Stop(ctx, StopRequest{ItemID: item.ID, UserID: item.UserID, KeyID: keyID, Reason: stopReason(cause)})
	case OutcomeRequeueBlocked:
		if e.Health != nil {
			if err := e.Health.ReportIPBlocked(ctx, keyID, cause); err != nil {
				e.logger().WarnContext(ctx, "crawl node health report failed", "work_item_id", item.ID, "error", err)
			}
		}
		return e.hold(ctx, item, timing.BlockedWait, cause.Error())
	default:
		if isRecoverableFlickrFailure(cause) {
			if item.Attempts+1 >= timing.RecoverableMaxAttempts {
				return e.Store.Fail(ctx, item.ID, "flickr unavailable after "+strconv.Itoa(timing.RecoverableMaxAttempts)+" attempts: "+cause.Error())
			}
			delay := min(timing.retryDelay(item.Attempts), MaxRetryDelay)
			return e.Store.Defer(ctx, item.ID, Deferral{NotBefore: e.now().Add(delay), ConsumeAttempt: true, Reason: cause.Error()})
		}
		if item.Attempts+1 >= timing.MaxAttempts {
			return e.Store.Fail(ctx, item.ID, cause.Error())
		}
		return e.Store.Defer(ctx, item.ID, Deferral{NotBefore: e.now().Add(timing.retryDelay(item.Attempts)), ConsumeAttempt: true, Reason: cause.Error()})
	}
}

// commit stores everything a page produced and moves the cursor in one unit
// of work. A page whose item moved on (finished, advanced or reclaimed) is
// skipped, so running a page twice changes nothing.
func (e *Engine) commit(ctx context.Context, item domain.Item, result domain.Result) error {
	maxDepth := 0
	if len(result.Discoveries) > 0 && item.UserID > 0 {
		effective, err := e.Settings.Effective(ctx, item.UserID)
		if err != nil {
			return fmt.Errorf("crawl: load max depth for user %d: %w", item.UserID, err)
		}
		maxDepth = effective.MaxDepth
	}
	return e.Units.Do(ctx, func(w Writers) error {
		current, err := w.Work.Lock(ctx, item.ID)
		if err != nil {
			return err
		}
		if current.State != domain.StateRunning || !sameCursor(current.Cursor, item.Cursor) {
			return nil
		}
		if err := e.write(ctx, w, item, result, maxDepth); err != nil {
			return err
		}
		if result.NextCursor != nil {
			return w.Work.Advance(ctx, item.ID, *result.NextCursor)
		}
		if result.HasMore {
			return w.Work.Advance(ctx, item.ID, item.Cursor.Next())
		}
		return w.Work.Finish(ctx, item.ID)
	})
}

func sameCursor(left, right domain.Cursor) bool {
	if left.Page != right.Page || left.Method != right.Method || left.CheckpointAt != right.CheckpointAt || len(left.Params) != len(right.Params) {
		return false
	}
	for key, value := range left.Params {
		if right.Params[key] != value {
			return false
		}
	}
	return true
}

func (e *Engine) write(ctx context.Context, w Writers, item domain.Item, result domain.Result, maxDepth int) error {
	if err := writeCatalog(ctx, w.Catalog, result); err != nil {
		return err
	}
	if result.CompleteReconcile {
		if w.Reconcile == nil || item.UserID <= 0 || item.Subject == "" || item.Cursor.CheckpointAt <= 0 {
			return errors.New("crawl: reconcile completion requires a valid work item and writer")
		}
		scannedAt := time.Unix(item.Cursor.CheckpointAt, 0).UTC()
		if err := w.Reconcile.Complete(ctx, item.UserID, item.Subject, scannedAt, e.now()); err != nil {
			return err
		}
	}
	if len(result.Observations) > 0 {
		if err := w.Observations.Record(ctx, item.ID, item.Cursor.Page, result.Observations); err != nil {
			return err
		}
	}
	if err := e.discover(ctx, w, item, result.Discoveries, maxDepth); err != nil {
		return err
	}
	for _, spec := range result.Work {
		if err := e.enqueue(ctx, w, item, spec, item.Depth); err != nil {
			return err
		}
	}
	return nil
}

func writeCatalog(ctx context.Context, writer CatalogWriter, result domain.Result) error {
	if len(result.People) > 0 {
		if err := writer.UpsertPeople(ctx, result.People); err != nil {
			return err
		}
	}
	if len(result.Photos) > 0 {
		if err := writer.UpsertPhotos(ctx, result.Photos); err != nil {
			return err
		}
	}
	if len(result.Favorites) > 0 {
		if err := writer.UpsertFavorites(ctx, result.Favorites); err != nil {
			return err
		}
	}
	if len(result.Albums) > 0 {
		if err := writer.UpsertAlbums(ctx, result.Albums); err != nil {
			return err
		}
	}
	if len(result.Galleries) > 0 {
		if err := writer.UpsertGalleries(ctx, result.Galleries); err != nil {
			return err
		}
	}
	if len(result.Groups) > 0 {
		if err := writer.UpsertGroups(ctx, result.Groups); err != nil {
			return err
		}
	}
	if len(result.PersonGroups) > 0 {
		if err := writer.UpsertPersonGroups(ctx, result.PersonGroups); err != nil {
			return err
		}
	}
	return nil
}

// discover records an interest one level deeper for each found person, unless
// that exceeds the user's maximum depth, and plans the person's work.
func (e *Engine) discover(ctx context.Context, w Writers, item domain.Item, found []domain.Discovery, maxDepth int) error {
	depth := item.Depth + 1
	if item.UserID <= 0 || depth > maxDepth {
		return nil
	}
	for _, discovery := range found {
		interest := domain.Interest{UserID: item.UserID, PersonNSID: discovery.NSID, Depth: depth, Via: discovery.Via}
		if err := interest.Validate(); err != nil {
			return err
		}
		if err := w.Interests.Add(ctx, interest); err != nil {
			return err
		}
		for _, spec := range e.Registry.Plan(ctx, domain.Person{NSID: discovery.NSID, UserID: item.UserID, Depth: depth}) {
			if err := e.enqueue(ctx, w, item, spec, depth); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine) enqueue(ctx context.Context, w Writers, item domain.Item, spec domain.WorkSpec, depth int) error {
	if spec.Kind == "" {
		spec.Kind = item.Kind
	}
	if _, ok := e.Registry.Get(spec.Kind); !ok {
		return fmt.Errorf("crawl: work for unknown facet %q", spec.Kind)
	}
	if err := spec.Validate(); err != nil {
		return err
	}
	return w.Work.Enqueue(ctx, NewWork{Kind: spec.Kind, Subject: spec.Subject, UserID: item.UserID, Depth: depth, Cursor: domain.NewCursor(spec), Reopen: spec.Reopen})
}

func (e *Engine) ready(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if e.Registry == nil || e.Store == nil || e.Units == nil || e.Keys == nil || e.Governor == nil || e.Flickr == nil || e.Settings == nil {
		return ErrEngineIncomplete
	}
	return ctx.Err()
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Engine) logger() *slog.Logger {
	if e.Logger != nil {
		return e.Logger
	}
	return slog.Default()
}
