package roles

import identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"

// mediaRouteAccess declares the auth requirement of the routes for downloads and the event stream, keyed by "METHOD pattern".
var mediaRouteAccess = map[string]identityhttp.Access{
	"POST /downloads":                                 identityhttp.AccessUser,
	"GET /downloads/{id}/parts":                       identityhttp.AccessUser,
	"POST /downloads/{id}/parts/{partNumber}/fetched": identityhttp.AccessUser,
	"POST /downloads/{id}/retry-failed":               identityhttp.AccessUser,
	"GET /events":                                     identityhttp.AccessUser,
}
