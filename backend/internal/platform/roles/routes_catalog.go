package roles

import identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"

// catalogRouteAccess declares the auth requirement of the routes for people, contacts, favorites, albums and photos, keyed by "METHOD pattern".
var catalogRouteAccess = map[string]identityhttp.Access{
	"GET /people":               identityhttp.AccessUser,
	"GET /contacts":             identityhttp.AccessUser,
	"GET /people/{nsid}":        identityhttp.AccessUser,
	"GET /people/{nsid}/photos": identityhttp.AccessUser,
	"GET /favorites":            identityhttp.AccessUser,
	"GET /me/albums":            identityhttp.AccessUser,
	"GET /albums":               identityhttp.AccessUser,
	"GET /albums/{id}/photos":   identityhttp.AccessUser,
	"GET /photos/{id}":          identityhttp.AccessUser,
}
