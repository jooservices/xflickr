package roles

import identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"

// identityRouteAccess declares the auth requirement of the routes for sessions, users, profile, keys, Flickr accounts, sharing, home and the public entry points, keyed by "METHOD pattern".
var identityRouteAccess = map[string]identityhttp.Access{
	"GET /api/v1":                        identityhttp.AccessPublic,
	"GET /healthz":                       identityhttp.AccessPublic,
	"GET /readyz":                        identityhttp.AccessPublic,
	"GET /api/v1/authentication-options": identityhttp.AccessPublic,
	"POST /api/v1/users":                 identityhttp.AccessPublic,
	"GET /api/v1/users/{id}":             identityhttp.AccessUser,
	"POST /api/v1/sessions":              identityhttp.AccessPublic,
	"DELETE /api/v1/sessions/current":    identityhttp.AccessPublic,
	"GET /api/v1/sessions/current":       identityhttp.AccessUser,
	"GET /api/v1/sessions/{id}":          identityhttp.AccessUser,
	"GET /api/v1/me":                     identityhttp.AccessUser,
	"PATCH /api/v1/me":                   identityhttp.AccessUser,
	"GET /api/v1/me/sessions":            identityhttp.AccessUser,
	"DELETE /api/v1/me/sessions":         identityhttp.AccessUser,
	"GET /api/v1/me/sessions/{id}":       identityhttp.AccessUser,
	"DELETE /api/v1/me/sessions/{id}":    identityhttp.AccessUser,
	"PUT /api/v1/me/password":            identityhttp.AccessUser,
	"GET /api/v1/me/sharing":             identityhttp.AccessUser,
	"PUT /api/v1/me/sharing":             identityhttp.AccessUser,
	"GET /api/v1/me/home":                identityhttp.AccessUser,
	"GET /keys":                          identityhttp.AccessUser,
	"POST /keys":                         identityhttp.AccessUser,
	"GET /keys/{id}":                     identityhttp.AccessUser,
	"PUT /keys/{id}":                     identityhttp.AccessUser,
	"DELETE /keys/{id}":                  identityhttp.AccessUser,
	"GET /keys/{id}/quota":               identityhttp.AccessUser,
	"POST /flickr-accounts/connect":      identityhttp.AccessUser,
	"GET /flickr-accounts":               identityhttp.AccessUser,
	"GET /flickr-accounts/callback":      identityhttp.AccessUser,
}
