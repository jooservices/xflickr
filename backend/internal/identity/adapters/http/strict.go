package http

import (
	"context"
	"errors"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

// StrictHandler lets identity tests construct the generated server. API key
// methods are implemented by the accounts module and composed in the API role.
type StrictHandler struct {
	Handler
}

var errAPIKeysServedByAccounts = errors.New("identity: api keys are served by the accounts module")

func (StrictHandler) ListKeys(context.Context, gen.ListKeysRequestObject) (gen.ListKeysResponseObject, error) {
	return nil, errAPIKeysServedByAccounts
}

func (StrictHandler) CreateKey(context.Context, gen.CreateKeyRequestObject) (gen.CreateKeyResponseObject, error) {
	return nil, errAPIKeysServedByAccounts
}

func (StrictHandler) DeleteKey(context.Context, gen.DeleteKeyRequestObject) (gen.DeleteKeyResponseObject, error) {
	return nil, errAPIKeysServedByAccounts
}

func (StrictHandler) GetKey(context.Context, gen.GetKeyRequestObject) (gen.GetKeyResponseObject, error) {
	return nil, errAPIKeysServedByAccounts
}

func (StrictHandler) UpdateKey(context.Context, gen.UpdateKeyRequestObject) (gen.UpdateKeyResponseObject, error) {
	return nil, errAPIKeysServedByAccounts
}

var errFlickrAccountsServedByAccounts = errors.New("identity: flickr accounts are served by the accounts module")

func (StrictHandler) ConnectFlickrAccount(context.Context, gen.ConnectFlickrAccountRequestObject) (gen.ConnectFlickrAccountResponseObject, error) {
	return nil, errFlickrAccountsServedByAccounts
}

func (StrictHandler) FlickrAccountCallback(context.Context, gen.FlickrAccountCallbackRequestObject) (gen.FlickrAccountCallbackResponseObject, error) {
	return nil, errFlickrAccountsServedByAccounts
}

var errPlansServedBySchedule = errors.New("identity: plans are served by the schedule module")

func (StrictHandler) GetAdminPlans(context.Context, gen.GetAdminPlansRequestObject) (gen.GetAdminPlansResponseObject, error) {
	return nil, errPlansServedBySchedule
}

func (StrictHandler) UpdateAdminPlans(context.Context, gen.UpdateAdminPlansRequestObject) (gen.UpdateAdminPlansResponseObject, error) {
	return nil, errPlansServedBySchedule
}

func (StrictHandler) UpdateAdminUserPlan(context.Context, gen.UpdateAdminUserPlanRequestObject) (gen.UpdateAdminUserPlanResponseObject, error) {
	return nil, errPlansServedBySchedule
}

func (StrictHandler) GetMeCrawlQueue(context.Context, gen.GetMeCrawlQueueRequestObject) (gen.GetMeCrawlQueueResponseObject, error) {
	return nil, errPlansServedBySchedule
}

func (StrictHandler) GetAdminQueue(context.Context, gen.GetAdminQueueRequestObject) (gen.GetAdminQueueResponseObject, error) {
	return nil, errPlansServedBySchedule
}

var errCrawlServedByCrawl = errors.New("identity: crawl controls are served by the crawl module")

func (StrictHandler) GetMeCrawl(context.Context, gen.GetMeCrawlRequestObject) (gen.GetMeCrawlResponseObject, error) {
	return nil, errCrawlServedByCrawl
}

func (StrictHandler) StarMyContact(context.Context, gen.StarMyContactRequestObject) (gen.StarMyContactResponseObject, error) {
	return nil, errCrawlServedByCrawl
}

func (StrictHandler) SelectMyContact(context.Context, gen.SelectMyContactRequestObject) (gen.SelectMyContactResponseObject, error) {
	return nil, errCrawlServedByCrawl
}

func (StrictHandler) PauseCrawlContact(context.Context, gen.PauseCrawlContactRequestObject) (gen.PauseCrawlContactResponseObject, error) {
	return nil, errCrawlServedByCrawl
}

func (StrictHandler) ResumeCrawlContact(context.Context, gen.ResumeCrawlContactRequestObject) (gen.ResumeCrawlContactResponseObject, error) {
	return nil, errCrawlServedByCrawl
}

var errQuotaServedByQuota = errors.New("identity: quota state is served by the quota module")

var errSharingServedBySharing = errors.New("identity: sharing settings are served by the sharing module")

func (StrictHandler) PutMeSharing(context.Context, gen.PutMeSharingRequestObject) (gen.PutMeSharingResponseObject, error) {
	return nil, errSharingServedBySharing
}

func (StrictHandler) GetKeyQuota(context.Context, gen.GetKeyQuotaRequestObject) (gen.GetKeyQuotaResponseObject, error) {
	return nil, errQuotaServedByQuota
}

func (StrictHandler) GetAdminQuota(context.Context, gen.GetAdminQuotaRequestObject) (gen.GetAdminQuotaResponseObject, error) {
	return nil, errQuotaServedByQuota
}
