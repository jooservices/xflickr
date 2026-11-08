package http

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

type (
	contextKey       struct{}
	remoteAddressKey struct{}
)

// WithUserID attaches the current user id for identity use cases.
func WithUserID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// WithRemoteAddress attaches the socket peer address without trusting proxy headers.
func WithRemoteAddress(ctx context.Context, address string) context.Context {
	return context.WithValue(ctx, remoteAddressKey{}, address)
}

// Middleware attaches the socket peer address to the request context. Forwarded
// headers are deliberately ignored because they are not trusted at this layer.
func Middleware(next gen.StrictHandlerFunc, _ string) gen.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		ctx = WithRemoteAddress(ctx, r.RemoteAddr)
		return next(ctx, w, r, request)
	}
}

func requestIP(ctx context.Context) string {
	value, _ := ctx.Value(remoteAddressKey{}).(string)
	return value
}

func clientIP(ctx context.Context) string {
	value := requestIP(ctx)
	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}
	return value
}

// UserLookup reads public identity data by id.
type UserLookup interface {
	FindByID(context.Context, int64) (domain.User, error)
}

// Handler adapts identity use cases to the versioned REST API.
type Handler struct {
	RegisterUseCase app.Register
	LoginUseCase    app.Login
	Users           UserLookup
	Sessions        SessionService
	AdminUsers      AdminUserService
	Profile         app.UpdateProfile
	Password        app.ChangePassword
	Version         string
}

func maskIP(value string) string {
	ip := net.ParseIP(value)
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.x", v4[0], v4[1], v4[2])
	}
	return ip.Mask(net.CIDRMask(48, 128)).String()
}

func userAgentLabel(value string) string {
	value = strings.ToLower(value)
	if value == "" {
		return "Unknown device"
	}
	browser := firstUserAgentMatch(value, "Browser", [][2]string{{"edg/", "Edge"}, {"firefox/", "Firefox"}, {"chromium/", "Chromium"}, {"chrome/", "Chrome"}, {"safari/", "Safari"}})
	os := firstUserAgentMatch(value, "", [][2]string{{"windows", "Windows"}, {"android", "Android"}, {"iphone", "iOS"}, {"ipad", "iOS"}, {"ios", "iOS"}, {"mac os", "macOS"}, {"macintosh", "macOS"}, {"linux", "Linux"}})
	if os == "" {
		return browser
	}
	return browser + " on " + os
}

func firstUserAgentMatch(value, fallback string, candidates [][2]string) string {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate[0]) {
			return candidate[1]
		}
	}
	return fallback
}
