package http

import (
	"context"
	"errors"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

func (StrictHandler) ListMyAlbums(context.Context, gen.ListMyAlbumsRequestObject) (gen.ListMyAlbumsResponseObject, error) {
	return nil, errCatalogServedByCatalog
}

func TestStrictHandlerLeavesContactAlbumsToCatalog(t *testing.T) {
	if _, err := (StrictHandler{}).ListMyAlbums(t.Context(), gen.ListMyAlbumsRequestObject{}); !errors.Is(err, errCatalogServedByCatalog) {
		t.Fatalf("list contact albums error = %v", err)
	}
}

func TestStrictHandlerReportsPlatformReadinessUnavailable(t *testing.T) {
	response, err := (StrictHandler{}).GetReadiness(t.Context(), gen.GetReadinessRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	unready, ok := response.(gen.GetReadiness503JSONResponse)
	if !ok || unready.Status != gen.ReadinessStatus("error") || len(unready.Dependencies) != 1 || unready.Dependencies[0].Name != "platform.startup" {
		t.Fatalf("readiness response = %#v", response)
	}
}

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
	if _, err := handler.ListFlickrAccounts(t.Context(), gen.ListFlickrAccountsRequestObject{}); !errors.Is(err, errFlickrAccountsServedByAccounts) {
		t.Fatalf("list flickr accounts error=%v", err)
	}
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
	if _, err := handler.GetMeCrawlQueue(t.Context(), gen.GetMeCrawlQueueRequestObject{}); !errors.Is(err, errPlansServedBySchedule) {
		t.Fatalf("get user queue error = %v", err)
	}
	if _, err := handler.GetAdminQueue(t.Context(), gen.GetAdminQueueRequestObject{}); !errors.Is(err, errPlansServedBySchedule) {
		t.Fatalf("get admin queue error = %v", err)
	}
}

func TestStrictHandlerLeavesCrawlControlsToCrawl(t *testing.T) {
	handler := StrictHandler{}
	if _, err := handler.GetMeCrawl(t.Context(), gen.GetMeCrawlRequestObject{}); !errors.Is(err, errCrawlServedByCrawl) {
		t.Fatalf("crawl status error=%v", err)
	}
	if _, err := handler.GetMeCrawlSettings(t.Context(), gen.GetMeCrawlSettingsRequestObject{}); !errors.Is(err, errCrawlServedByCrawl) {
		t.Fatalf("crawl settings error=%v", err)
	}
	if _, err := handler.UpdateMeCrawlSettings(t.Context(), gen.UpdateMeCrawlSettingsRequestObject{}); !errors.Is(err, errCrawlServedByCrawl) {
		t.Fatalf("update crawl settings error=%v", err)
	}
	if _, err := handler.StarMyContact(t.Context(), gen.StarMyContactRequestObject{}); !errors.Is(err, errCrawlServedByCrawl) {
		t.Fatalf("star error=%v", err)
	}
	if _, err := handler.SelectMyContact(t.Context(), gen.SelectMyContactRequestObject{}); !errors.Is(err, errCrawlServedByCrawl) {
		t.Fatalf("select error=%v", err)
	}
	if _, err := handler.PauseCrawlContact(t.Context(), gen.PauseCrawlContactRequestObject{}); !errors.Is(err, errCrawlServedByCrawl) {
		t.Fatalf("pause error=%v", err)
	}
	if _, err := handler.ResumeCrawlContact(t.Context(), gen.ResumeCrawlContactRequestObject{}); !errors.Is(err, errCrawlServedByCrawl) {
		t.Fatalf("resume error=%v", err)
	}
}

func TestStrictHandlerLeavesCatalogReadsToCatalog(t *testing.T) {
	handler := StrictHandler{}
	if _, err := handler.ListPeople(t.Context(), gen.ListPeopleRequestObject{}); !errors.Is(err, errCatalogServedByCatalog) {
		t.Fatalf("list people error = %v", err)
	}
	if _, err := handler.GetPerson(t.Context(), gen.GetPersonRequestObject{}); !errors.Is(err, errCatalogServedByCatalog) {
		t.Fatalf("get person error = %v", err)
	}
	if _, err := handler.ListPersonPhotos(t.Context(), gen.ListPersonPhotosRequestObject{}); !errors.Is(err, errCatalogServedByCatalog) {
		t.Fatalf("list person photos error = %v", err)
	}
	if _, err := handler.ListFavorites(t.Context(), gen.ListFavoritesRequestObject{}); !errors.Is(err, errCatalogServedByCatalog) {
		t.Fatalf("list favorites error = %v", err)
	}
	if _, err := handler.ListAlbums(t.Context(), gen.ListAlbumsRequestObject{}); !errors.Is(err, errCatalogServedByCatalog) {
		t.Fatalf("list albums error = %v", err)
	}
	if _, err := handler.ListAlbumPhotos(t.Context(), gen.ListAlbumPhotosRequestObject{}); !errors.Is(err, errCatalogServedByCatalog) {
		t.Fatalf("list album photos error = %v", err)
	}
	if _, err := handler.GetPhoto(t.Context(), gen.GetPhotoRequestObject{}); !errors.Is(err, errCatalogServedByCatalog) {
		t.Fatalf("get photo error = %v", err)
	}
}

func TestStrictHandlerLeavesDownloadsToMedia(t *testing.T) {
	handler := StrictHandler{}
	if _, err := handler.CreateDownload(t.Context(), gen.CreateDownloadRequestObject{}); !errors.Is(err, errDownloadsServedByMedia) {
		t.Fatalf("create download error = %v", err)
	}
	if _, err := handler.GetDownloadParts(t.Context(), gen.GetDownloadPartsRequestObject{}); !errors.Is(err, errDownloadsServedByMedia) {
		t.Fatalf("get download parts error = %v", err)
	}
	if _, err := handler.MarkDownloadPartFetched(t.Context(), gen.MarkDownloadPartFetchedRequestObject{}); !errors.Is(err, errDownloadsServedByMedia) {
		t.Fatalf("mark download part fetched error = %v", err)
	}
	if _, err := handler.RetryFailedDownload(t.Context(), gen.RetryFailedDownloadRequestObject{}); !errors.Is(err, errDownloadsServedByMedia) {
		t.Fatalf("retry failed download error = %v", err)
	}
}

func TestStrictHandlerLeavesEventStreamToPlatform(t *testing.T) {
	if _, err := (StrictHandler{}).StreamEvents(t.Context(), gen.StreamEventsRequestObject{}); !errors.Is(err, errEventsServedByPlatform) {
		t.Fatalf("stream events error = %v", err)
	}
}

func TestStrictHandlerLeavesSharingToSharing(t *testing.T) {
	if _, err := (StrictHandler{}).PutMeSharing(t.Context(), gen.PutMeSharingRequestObject{}); !errors.Is(err, errSharingServedBySharing) {
		t.Fatalf("sharing error = %v", err)
	}
}

func TestStrictHandlerLeavesQuotaToQuota(t *testing.T) {
	handler := StrictHandler{}
	if _, err := handler.GetKeyQuota(t.Context(), gen.GetKeyQuotaRequestObject{}); !errors.Is(err, errQuotaServedByQuota) {
		t.Fatalf("key quota error=%v", err)
	}
	if _, err := handler.GetAdminQuota(t.Context(), gen.GetAdminQuotaRequestObject{}); !errors.Is(err, errQuotaServedByQuota) {
		t.Fatalf("admin quota error=%v", err)
	}
	if _, err := handler.GetAdminCallStats(t.Context(), gen.GetAdminCallStatsRequestObject{}); !errors.Is(err, errQuotaServedByQuota) {
		t.Fatalf("admin call stats error=%v", err)
	}
}

func TestStrictHandlerLeavesOpsToOps(t *testing.T) {
	handler := StrictHandler{}
	if _, err := handler.GetAdminOpsStatus(t.Context(), gen.GetAdminOpsStatusRequestObject{}); !errors.Is(err, errOpsServedByOps) {
		t.Fatalf("ops status error=%v", err)
	}
	if _, err := handler.GetAdminMetrics(t.Context(), gen.GetAdminMetricsRequestObject{}); !errors.Is(err, errOpsServedByOps) {
		t.Fatalf("admin metrics error=%v", err)
	}
}

func TestStrictHandlerLeavesFeatureFlagsToSettings(t *testing.T) {
	handler := StrictHandler{}
	if _, err := handler.GetAdminFlag(t.Context(), gen.GetAdminFlagRequestObject{}); !errors.Is(err, errFeatureFlagsServedBySettings) {
		t.Fatalf("get flag error = %v", err)
	}
	if _, err := handler.GetAdminFlags(t.Context(), gen.GetAdminFlagsRequestObject{}); !errors.Is(err, errFeatureFlagsServedBySettings) {
		t.Fatalf("get flags error = %v", err)
	}
	if _, err := handler.UpdateAdminFlags(t.Context(), gen.UpdateAdminFlagsRequestObject{}); !errors.Is(err, errFeatureFlagsServedBySettings) {
		t.Fatalf("update flags error = %v", err)
	}
}

func TestStrictHandlerLeavesNodeEndpointsToCluster(t *testing.T) {
	handler := StrictHandler{}
	if _, err := handler.CreateAdminNode(t.Context(), gen.CreateAdminNodeRequestObject{}); !errors.Is(err, errNodesServedByCluster) {
		t.Fatalf("create node error = %v", err)
	}
	if _, err := handler.HeartbeatNode(t.Context(), gen.HeartbeatNodeRequestObject{}); !errors.Is(err, errNodesServedByCluster) {
		t.Fatalf("heartbeat error = %v", err)
	}
}
