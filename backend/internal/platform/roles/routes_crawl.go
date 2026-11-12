package roles

import identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"

// crawlRouteAccess declares the auth requirement of the routes for crawl status, settings and contact selection, keyed by "METHOD pattern".
var crawlRouteAccess = map[string]identityhttp.Access{
	"GET /me/crawl":                  identityhttp.AccessUser,
	"GET /me/crawl/queue":            identityhttp.AccessUser,
	"GET /me/crawl/settings":         identityhttp.AccessUser,
	"PUT /me/crawl/settings":         identityhttp.AccessUser,
	"PUT /me/crawl/pause":            identityhttp.AccessUser,
	"PUT /me/crawl/resume":           identityhttp.AccessUser,
	"PUT /me/contacts/{nsid}/star":   identityhttp.AccessUser,
	"PUT /me/contacts/{nsid}/select": identityhttp.AccessUser,
}
