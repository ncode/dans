package httpserver

import (
	"context"
	"errors"
	"net/http"

	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/httpapi"
	"github.com/ncode/dans/internal/identifier"
)

// Authenticator resolves the current durable actor state for one plaintext token.
type Authenticator interface {
	Authenticate(context.Context, string) (database.Actor, error)
}

// Authentication enforces exactly one syntactically valid DANS token on every
// route except the two root health probes.
func Authentication(authenticator Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/livez" || request.URL.Path == "/readyz" {
				next.ServeHTTP(w, request)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			values := request.Header.Values("X-API-Key")
			if len(values) != 1 || identifier.ValidateToken(values[0]) != nil {
				httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindUnauthenticated, database.ErrUnauthenticated))
				return
			}
			if authenticator == nil {
				httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindUnavailable, errors.New("authentication dependency is not configured")))
				return
			}
			actor, err := authenticator.Authenticate(request.Context(), values[0])
			if errors.Is(err, database.ErrUnauthenticated) {
				httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindUnauthenticated, err))
				return
			}
			if err != nil {
				httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindUnavailable, err))
				return
			}
			ctx := context.WithValue(request.Context(), actorKey, actor)
			next.ServeHTTP(w, request.WithContext(ctx))
		})
	}
}

// ActorFromContext returns the actor established by Authentication.
func ActorFromContext(ctx context.Context) (database.Actor, bool) {
	actor, ok := ctx.Value(actorKey).(database.Actor)
	return actor, ok
}
