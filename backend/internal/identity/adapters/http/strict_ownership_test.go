package http

import (
	"errors"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

func TestStrictHandlerDelegatesEveryNonContactRoute(t *testing.T) {
	handler := StrictHandler{}
	cases := []struct {
		name string
		want error
		call func() error
	}{
		{"list keys", errAPIKeysServedByAccounts, func() error { _, err := handler.ListKeys(t.Context(), gen.ListKeysRequestObject{}); return err }},
		{"create key", errAPIKeysServedByAccounts, func() error { _, err := handler.CreateKey(t.Context(), gen.CreateKeyRequestObject{}); return err }},
		{"delete key", errAPIKeysServedByAccounts, func() error { _, err := handler.DeleteKey(t.Context(), gen.DeleteKeyRequestObject{}); return err }},
		{"get key", errAPIKeysServedByAccounts, func() error { _, err := handler.GetKey(t.Context(), gen.GetKeyRequestObject{}); return err }},
		{"update key", errAPIKeysServedByAccounts, func() error { _, err := handler.UpdateKey(t.Context(), gen.UpdateKeyRequestObject{}); return err }},
		{"list Flickr accounts", errFlickrAccountsServedByAccounts, func() error {
			_, err := handler.ListFlickrAccounts(t.Context(), gen.ListFlickrAccountsRequestObject{})
			return err
		}},
		{"connect Flickr account", errFlickrAccountsServedByAccounts, func() error {
			_, err := handler.ConnectFlickrAccount(t.Context(), gen.ConnectFlickrAccountRequestObject{})
			return err
		}},
		{"Flickr callback", errFlickrAccountsServedByAccounts, func() error {
			_, err := handler.FlickrAccountCallback(t.Context(), gen.FlickrAccountCallbackRequestObject{})
			return err
		}},
		{"get admin plans", errPlansServedBySchedule, func() error {
			_, err := handler.GetAdminPlans(t.Context(), gen.GetAdminPlansRequestObject{})
			return err
		}},
		{"update admin plans", errPlansServedBySchedule, func() error {
			_, err := handler.UpdateAdminPlans(t.Context(), gen.UpdateAdminPlansRequestObject{})
			return err
		}},
		{"update admin user plan", errPlansServedBySchedule, func() error {
			_, err := handler.UpdateAdminUserPlan(t.Context(), gen.UpdateAdminUserPlanRequestObject{})
			return err
		}},
		{"get crawl queue", errPlansServedBySchedule, func() error {
			_, err := handler.GetMeCrawlQueue(t.Context(), gen.GetMeCrawlQueueRequestObject{})
			return err
		}},
		{"get admin queue", errPlansServedBySchedule, func() error {
			_, err := handler.GetAdminQueue(t.Context(), gen.GetAdminQueueRequestObject{})
			return err
		}},
		{"get crawl settings", errCrawlServedByCrawl, func() error {
			_, err := handler.GetMeCrawlSettings(t.Context(), gen.GetMeCrawlSettingsRequestObject{})
			return err
		}},
		{"update crawl settings", errCrawlServedByCrawl, func() error {
			_, err := handler.UpdateMeCrawlSettings(t.Context(), gen.UpdateMeCrawlSettingsRequestObject{})
			return err
		}},
		{"get crawl summary", errCrawlServedByCrawl, func() error { _, err := handler.GetMeCrawl(t.Context(), gen.GetMeCrawlRequestObject{}); return err }},
		{"list people", errCatalogServedByCatalog, func() error { _, err := handler.ListPeople(t.Context(), gen.ListPeopleRequestObject{}); return err }},
		{"get person", errCatalogServedByCatalog, func() error { _, err := handler.GetPerson(t.Context(), gen.GetPersonRequestObject{}); return err }},
		{"list person photos", errCatalogServedByCatalog, func() error {
			_, err := handler.ListPersonPhotos(t.Context(), gen.ListPersonPhotosRequestObject{})
			return err
		}},
		{"list favorites", errCatalogServedByCatalog, func() error {
			_, err := handler.ListFavorites(t.Context(), gen.ListFavoritesRequestObject{})
			return err
		}},
		{"list albums", errCatalogServedByCatalog, func() error { _, err := handler.ListAlbums(t.Context(), gen.ListAlbumsRequestObject{}); return err }},
		{"list album photos", errCatalogServedByCatalog, func() error {
			_, err := handler.ListAlbumPhotos(t.Context(), gen.ListAlbumPhotosRequestObject{})
			return err
		}},
		{"get photo", errCatalogServedByCatalog, func() error { _, err := handler.GetPhoto(t.Context(), gen.GetPhotoRequestObject{}); return err }},
		{"create download", errDownloadsServedByMedia, func() error {
			_, err := handler.CreateDownload(t.Context(), gen.CreateDownloadRequestObject{})
			return err
		}},
		{"get download parts", errDownloadsServedByMedia, func() error {
			_, err := handler.GetDownloadParts(t.Context(), gen.GetDownloadPartsRequestObject{})
			return err
		}},
		{"mark download part fetched", errDownloadsServedByMedia, func() error {
			_, err := handler.MarkDownloadPartFetched(t.Context(), gen.MarkDownloadPartFetchedRequestObject{})
			return err
		}},
		{"retry failed download", errDownloadsServedByMedia, func() error {
			_, err := handler.RetryFailedDownload(t.Context(), gen.RetryFailedDownloadRequestObject{})
			return err
		}},
		{"stream events", errEventsServedByPlatform, func() error { _, err := handler.StreamEvents(t.Context(), gen.StreamEventsRequestObject{}); return err }},
		{"update sharing", errSharingServedBySharing, func() error { _, err := handler.PutMeSharing(t.Context(), gen.PutMeSharingRequestObject{}); return err }},
		{"get key quota", errQuotaServedByQuota, func() error { _, err := handler.GetKeyQuota(t.Context(), gen.GetKeyQuotaRequestObject{}); return err }},
		{"get admin quota", errQuotaServedByQuota, func() error {
			_, err := handler.GetAdminQuota(t.Context(), gen.GetAdminQuotaRequestObject{})
			return err
		}},
		{"get admin call stats", errQuotaServedByQuota, func() error {
			_, err := handler.GetAdminCallStats(t.Context(), gen.GetAdminCallStatsRequestObject{})
			return err
		}},
		{"get admin ops status", errOpsServedByOps, func() error {
			_, err := handler.GetAdminOpsStatus(t.Context(), gen.GetAdminOpsStatusRequestObject{})
			return err
		}},
		{"get admin flags", errFeatureFlagsServedBySettings, func() error {
			_, err := handler.GetAdminFlags(t.Context(), gen.GetAdminFlagsRequestObject{})
			return err
		}},
		{"update admin flags", errFeatureFlagsServedBySettings, func() error {
			_, err := handler.UpdateAdminFlags(t.Context(), gen.UpdateAdminFlagsRequestObject{})
			return err
		}},
		{"create admin node", errNodesServedByCluster, func() error {
			_, err := handler.CreateAdminNode(t.Context(), gen.CreateAdminNodeRequestObject{})
			return err
		}},
		{"heartbeat node", errNodesServedByCluster, func() error {
			_, err := handler.HeartbeatNode(t.Context(), gen.HeartbeatNodeRequestObject{})
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, tc.want) {
				t.Fatalf("route error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestStrictHandlerReportsReadinessUnavailable(t *testing.T) {
	response, err := (StrictHandler{}).GetReadiness(t.Context(), gen.GetReadinessRequestObject{})
	if err != nil {
		t.Fatalf("readiness error = %v", err)
	}
	body, ok := response.(gen.GetReadiness503JSONResponse)
	if !ok {
		t.Fatalf("readiness response type = %T", response)
	}
	if body.Status != gen.ReadinessStatus("error") || len(body.Dependencies) != 1 || body.Dependencies[0].Name != "platform.startup" {
		t.Fatalf("readiness response = %#v", body)
	}
}
