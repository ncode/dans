package httpserver

import (
	"errors"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/ncode/dans/internal/contract"
	"github.com/ncode/dans/internal/httpapi"
	"github.com/ncode/dans/internal/upstream"
)

// PowerDNSCompatibility records only the latest bounded compatibility probe.
// It starts fail-closed until the first successful probe.
type PowerDNSCompatibility struct {
	compatible atomic.Bool
}

func (state *PowerDNSCompatibility) Record(err error) {
	if err == nil {
		state.compatible.Store(true)
		return
	}
	if errors.Is(err, upstream.ErrIncompatibleVersion) {
		state.compatible.Store(false)
	}
}

func (state *PowerDNSCompatibility) Compatible() bool {
	return state != nil && state.compatible.Load()
}

type CompatibilityConfig struct {
	Schema             Probe
	PowerDNSCompatible func() bool
}

// Compatibility prevents authenticated traffic and sign-in from reaching handlers while
// the database/schema is unusable, and prevents compatibility requests from
// reaching an upstream outside the pinned PowerDNS surface.
func Compatibility(config CompatibilityConfig) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			route, ok := RouteInfoFromContext(request.Context())
			if !ok {
				httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindUnavailable, errors.New("request route is not classified")))
				return
			}
			if route.AccessClass == contract.AccessAnonymous && route.OperationID != "createBrowserSession" {
				next.ServeHTTP(w, request)
				return
			}
			if config.Schema == nil || config.Schema(request.Context()) != nil {
				httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindUnavailable, errors.New("runtime compatibility is unavailable")))
				return
			}
			if isPowerDNSRoute(route.Template) && (config.PowerDNSCompatible == nil || !config.PowerDNSCompatible()) {
				httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindUnavailable, errors.New("PowerDNS compatibility is unavailable")))
				return
			}
			next.ServeHTTP(w, request)
		})
	}
}

func isPowerDNSRoute(template string) bool {
	return template == "/api/v1/error" || strings.HasPrefix(template, "/api/v1/servers") || strings.HasPrefix(template, "/api/v1/dans/servers/")
}
