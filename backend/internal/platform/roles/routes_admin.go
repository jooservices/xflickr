package roles

import identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"

// adminRouteAccess declares the auth requirement of the routes for administrator and node-internal routes, keyed by "METHOD pattern".
var adminRouteAccess = map[string]identityhttp.Access{
	"GET /admin/users":               identityhttp.AccessAdmin,
	"PATCH /admin/users/{id}":        identityhttp.AccessAdmin,
	"PUT /admin/users/{id}/plan":     identityhttp.AccessAdmin,
	"GET /admin/plans":               identityhttp.AccessAdmin,
	"PUT /admin/plans":               identityhttp.AccessAdmin,
	"GET /admin/flags":               identityhttp.AccessAdmin,
	"PUT /admin/flags":               identityhttp.AccessAdmin,
	"GET /api/v1/admin/flags/{name}": identityhttp.AccessPublic,
	"GET /admin/queue":               identityhttp.AccessAdmin,
	"GET /admin/quota":               identityhttp.AccessAdmin,
	"GET /admin/calls/stats":         identityhttp.AccessAdmin,
	"GET /admin/metrics":             identityhttp.AccessAdmin,
	"GET /admin/ops/status":          identityhttp.AccessAdmin,
	"GET /admin/incidents":           identityhttp.AccessAdmin,
	"POST /admin/nodes":              identityhttp.AccessAdmin,
	"POST /internal/nodes/heartbeat": identityhttp.AccessPublic,
}
