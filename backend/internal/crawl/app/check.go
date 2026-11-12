package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/jooservices/xflickr/internal/crawl/domain"
)

const (
	incrementalPerPage     = 500
	incrementalPhotoExtras = "url_sq,url_t,url_s,url_q,url_m,url_n,url_z,url_c,url_l,url_o,date_upload,last_update,owner_name,media,tags,description,license,o_dims,views"
	ReconcilePhotoExtras   = incrementalPhotoExtras
)

// ChangeCheck handles both probes in one durable per-person work item.
type ChangeCheck struct{}

func (ChangeCheck) Name() string                                          { return "check" }
func (ChangeCheck) Lane() domain.Lane                                     { return domain.LanePerson }
func (ChangeCheck) Plan(context.Context, domain.Person) []domain.WorkSpec { return nil }

// Handle plans matching incremental work after each probe. The photo probe
// advances the same item to the favorite probe, preserving one check item per
// person while each changed list remains a separate paged work item.
func (ChangeCheck) Handle(_ context.Context, page domain.Page) (domain.Result, error) {
	total, err := resultTotal(page.Payload)
	if err != nil {
		return domain.Result{}, fmt.Errorf("crawl: decode change check: %w", err)
	}
	return changeCheckResult(page, total)
}

// Skipped advances an inaccessible probe without interpreting it as a successful scan.
func (ChangeCheck) Skipped(_ context.Context, page domain.Page) (domain.Result, error) {
	return changeCheckResult(page, 0)
}

func changeCheckResult(page domain.Page, total int64) (domain.Result, error) {
	params := page.Item.Cursor.Params
	if params == nil {
		params = map[string]string{}
	}
	checkpoint := page.Item.Cursor.CheckpointAt
	switch page.Item.Cursor.Method {
	case "flickr.people.getPhotos":
		result := domain.Result{NextWorkCursor: &domain.Cursor{Page: 1, Method: "flickr.favorites.getList", CheckpointAt: checkpoint, Params: map[string]string{
			"user_id": page.Item.Subject, domain.ParamMinFaveDate: params[domain.ParamMinFaveDate], "per_page": "1",
		}}}
		if total > 0 {
			result.Work = append(result.Work, incrementalListWork(domain.FacetPhotos, page.Item.Subject, checkpoint, params[domain.ParamMinUploadDate]))
		}
		return result, nil
	case "flickr.favorites.getList":
		result := domain.Result{Work: catalogRefreshWork(page.Item.Subject)}
		if total > 0 {
			result.Work = append(result.Work, incrementalListWork(domain.FacetFavorites, page.Item.Subject, checkpoint, params[domain.ParamMinFaveDate]))
		}
		return result, nil
	default:
		return domain.Result{}, fmt.Errorf("crawl: unsupported check method %q", page.Item.Cursor.Method)
	}
}

func incrementalListWork(kind, person string, checkpointAt int64, minDate string) domain.WorkSpec {
	params := map[string]string{
		"user_id": person, "per_page": strconv.Itoa(incrementalPerPage), "extras": incrementalPhotoExtras,
	}
	method := "flickr.people.getPhotos"
	if kind == domain.FacetFavorites {
		method = "flickr.favorites.getList"
		params[domain.ParamMinFaveDate] = minDate
	} else {
		params[domain.ParamMinUploadDate] = minDate
	}
	return domain.WorkSpec{Kind: kind, Subject: person, Method: method, CheckpointAt: checkpointAt, Reopen: true, Params: params}
}

// catalogRefreshWork re-reads the albums, galleries and groups lists. It runs
// only from the second probe of a check item, and a person's check item is
// itself gated by CheckIntervalDue, so each catalog is reopened at most once
// per check cycle (not per favorites page). Flickr offers no cheap change
// probe for these lists; the interval gate is what bounds the quota spent.
func catalogRefreshWork(person string) []domain.WorkSpec {
	return []domain.WorkSpec{
		{Kind: "albums", Subject: person, Method: "flickr.photosets.getList", Reopen: true, Params: map[string]string{"user_id": person, "per_page": strconv.Itoa(incrementalPerPage)}},
		{Kind: "galleries", Subject: person, Method: "flickr.galleries.getList", Reopen: true, Params: map[string]string{"user_id": person, "per_page": strconv.Itoa(incrementalPerPage), "continuation": "0"}},
		{Kind: "groups", Subject: person, Method: "flickr.people.getPublicGroups", Reopen: true, Params: map[string]string{"user_id": person, "per_page": strconv.Itoa(incrementalPerPage)}},
	}
}

// ContactsPhotos handles one account's recent contacts response and queues a
// cheap change check for every distinct owner in that response.
type ContactsPhotos struct{}

func (ContactsPhotos) Name() string {
	return "contacts_photos"
}

func (ContactsPhotos) Lane() domain.Lane {
	return domain.LanePerson
}

func (ContactsPhotos) Plan(context.Context, domain.Person) []domain.WorkSpec {
	return nil
}

func (ContactsPhotos) Handle(_ context.Context, page domain.Page) (domain.Result, error) {
	people, err := FlaggedPeopleFromContactsPhotos(page.Payload)
	if err != nil {
		return domain.Result{}, err
	}
	result := domain.Result{}
	for _, person := range people {
		result.Work = append(result.Work, domain.WorkSpec{Kind: "check", Subject: person, Method: "flickr.people.getPhotos", CheckpointAt: page.Item.Cursor.CheckpointAt, Reopen: true, Params: map[string]string{
			"user_id": person, "min_upload_date": "0", "min_fave_date": "0", "per_page": "1",
		}})
	}
	return result, nil
}

// CheckRequests returns the two inexpensive Flickr probes for one person.
// lastSeen and lastFavorite are Unix seconds from that user's crawl checkpoint.
func CheckRequests(person string, lastSeen, lastFavorite int64) []RequestSpec {
	photoParams := domain.PhotosCheckpointFilter(domain.ScanCheckpoint(lastSeen))
	photoParams["user_id"] = person
	photoParams["per_page"] = "1"
	faveParams := domain.FavoritesCheckpointFilter(domain.ScanCheckpoint(lastFavorite))
	faveParams["user_id"] = person
	faveParams["per_page"] = "1"
	return []RequestSpec{
		{Kind: domain.FacetPhotos, Subject: person, Method: "flickr.people.getPhotos", Params: photoParams},
		{Kind: domain.FacetFavorites, Subject: person, Method: "flickr.favorites.getList", Params: faveParams},
	}
}

// RequestSpec describes one Flickr request generated by a change check.
type RequestSpec struct {
	Kind, Subject, Method string
	Params                map[string]string
}

// IncrementalWork creates paged work only for probes that report results.
// Flickr returns total as either a JSON number or a decimal string.
func IncrementalWork(person string, lastSeen, lastFavorite int64, photos, favorites json.RawMessage) ([]domain.WorkSpec, error) {
	photoCount, err := resultTotal(photos)
	if err != nil {
		return nil, fmt.Errorf("crawl: decode photo check: %w", err)
	}
	favoriteCount, err := resultTotal(favorites)
	if err != nil {
		return nil, fmt.Errorf("crawl: decode favorite check: %w", err)
	}
	result := make([]domain.WorkSpec, 0, 2)
	if photoCount > 0 {
		result = append(result, incrementalListWork(domain.FacetPhotos, person, 0, strconv.FormatInt(lastSeen, 10)))
	}
	if favoriteCount > 0 {
		result = append(result, incrementalListWork(domain.FacetFavorites, person, 0, strconv.FormatInt(lastFavorite, 10)))
	}
	return result, nil
}

func resultTotal(payload json.RawMessage) (int64, error) {
	var envelope struct {
		Photos struct {
			Total json.RawMessage `json:"total"`
		} `json:"photos"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return 0, err
	}
	if len(envelope.Photos.Total) == 0 {
		return 0, nil
	}
	var text string
	if envelope.Photos.Total[0] == '"' {
		if err := json.Unmarshal(envelope.Photos.Total, &text); err != nil {
			return 0, err
		}
	} else {
		text = string(envelope.Photos.Total)
	}
	total, err := strconv.ParseInt(text, 10, 64)
	if err != nil || total < 0 {
		return 0, fmt.Errorf("invalid total %q", text)
	}
	return total, nil
}

// ContactsPhotosRequest asks Flickr to return recent photos from the signed-in
// user's direct contacts. One response can identify many people to check.
func ContactsPhotosRequest(accountNSID string) RequestSpec {
	return RequestSpec{Kind: "contacts_photos", Subject: accountNSID, Method: "flickr.photos.getContactsPhotos", Params: map[string]string{"user_id": accountNSID, "count": "50"}}
}

// FlaggedPeopleFromContactsPhotos returns the distinct owners represented in
// a contacts-photos response, allowing one Flickr call to flag several people.
func FlaggedPeopleFromContactsPhotos(payload json.RawMessage) ([]string, error) {
	var envelope struct {
		Photos struct {
			Photo []struct {
				Owner string `json:"owner"`
			} `json:"photo"`
		} `json:"photos"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, fmt.Errorf("crawl: decode contacts photos: %w", err)
	}
	seen := make(map[string]struct{}, len(envelope.Photos.Photo))
	people := make([]string, 0, len(envelope.Photos.Photo))
	for _, photo := range envelope.Photos.Photo {
		if photo.Owner == "" {
			continue
		}
		if _, ok := seen[photo.Owner]; ok {
			continue
		}
		seen[photo.Owner] = struct{}{}
		people = append(people, photo.Owner)
	}
	return people, nil
}

// CheckIntervalDue reports whether a person may be checked again.
func CheckIntervalDue(lastChecked, now time.Time, interval time.Duration) bool {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	return lastChecked.IsZero() || !now.Before(lastChecked.Add(interval))
}
