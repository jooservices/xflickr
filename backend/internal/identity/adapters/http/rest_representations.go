package http

import (
	"net/http"
	"strconv"

	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/api/rest"
)

func (response *apiResponse) VisitGetAuthConfigResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitRegisterResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitLoginResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitLogoutResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitRevokeOtherSessionsResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitGetMeResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitUpdateMeResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitListMySessionsResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitRevokeMySessionResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitChangeMyPasswordResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitGetUserResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitGetCurrentSessionResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitGetSessionResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitGetMySessionResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func authOptionsLinks() rest.Links {
	return rest.Links{
		"self":     rest.NewLink("/api/v1/authentication-options", http.MethodGet),
		"users":    rest.NewLink("/api/v1/users", http.MethodPost),
		"sessions": rest.NewLink("/api/v1/sessions", http.MethodPost),
	}
}

func userLinks(self string) rest.Links {
	return rest.Links{
		"self":     rest.NewLink(self, http.MethodGet),
		"me":       rest.NewLink("/api/v1/me", http.MethodGet),
		"sessions": rest.NewLink("/api/v1/sessions", http.MethodPost),
	}
}

func meLinks() rest.Links {
	return rest.Links{
		"self":                  rest.NewLink("/api/v1/me", http.MethodGet),
		"update":                rest.NewLink("/api/v1/me", http.MethodPatch),
		"sessions":              rest.NewLink("/api/v1/me/sessions", http.MethodGet),
		"revoke-other-sessions": rest.NewLink("/api/v1/me/sessions?scope=others", http.MethodDelete),
		"password":              rest.NewLink("/api/v1/me/password", http.MethodPut),
		"sharing":               rest.NewLink("/api/v1/me/sharing", http.MethodGet),
		"update-sharing":        rest.NewLink("/api/v1/me/sharing", http.MethodPut),
		"home":                  rest.NewLink("/api/v1/me/home", http.MethodGet),
	}
}

func userRepresentation(user domain.User, links rest.Links) map[string]any {
	return map[string]any{
		"id": user.ID, "email": user.Email, "role": string(user.Role),
		"locale": user.Locale, "created_at": user.CreatedAt.UTC(), "links": links,
	}
}

func publicUserRepresentation(user domain.User, self string) map[string]any {
	return map[string]any{
		"id": user.ID, "role": string(user.Role), "locale": user.Locale,
		"created_at": user.CreatedAt.UTC(), "links": rest.Links{"self": rest.NewLink(self, http.MethodGet)},
	}
}

func sessionLinks(session domain.Session, current bool, href string) rest.Links {
	links := rest.Links{
		"self":    rest.NewLink(href, http.MethodGet),
		"session": rest.NewLink("/api/v1/sessions/"+strconv.FormatInt(session.ID, 10), http.MethodGet),
		"user":    rest.NewLink("/api/v1/users/"+strconv.FormatInt(session.UserID, 10), http.MethodGet),
	}
	if current {
		links["revoke"] = rest.NewLink("/api/v1/sessions/current", http.MethodDelete)
	} else {
		links["revoke"] = rest.NewLink("/api/v1/me/sessions/"+strconv.FormatInt(session.ID, 10), http.MethodDelete)
	}
	return links
}

func sessionSummary(session domain.Session, current bool) map[string]any {
	href := "/api/v1/me/sessions/" + strconv.FormatInt(session.ID, 10)
	return map[string]any{
		"id": strconv.FormatInt(session.ID, 10), "current": current,
		"user_agent_label": userAgentLabel(session.UserAgent), "ip_masked": maskIP(session.IPAddress),
		"last_seen_at": session.LastSeenAt.UTC(), "created_at": session.CreatedAt.UTC(),
		"expires_at": session.ExpiresAt.UTC(), "links": sessionLinks(session, current, href),
	}
}

func sessionRepresentation(session domain.Session, user domain.User, current bool, selfPath string) map[string]any {
	href := "/api/v1/sessions/" + strconv.FormatInt(session.ID, 10)
	if current {
		href = "/api/v1/sessions/current"
	}
	if selfPath != "" {
		href = selfPath
	}
	return map[string]any{
		"id": strconv.FormatInt(session.ID, 10), "current": current,
		"user_agent_label": userAgentLabel(session.UserAgent), "ip_masked": maskIP(session.IPAddress),
		"last_seen_at": session.LastSeenAt.UTC(), "created_at": session.CreatedAt.UTC(),
		"expires_at": session.ExpiresAt.UTC(), "links": sessionLinks(session, current, href),
		"user": userRepresentation(user, meLinks()),
	}
}
