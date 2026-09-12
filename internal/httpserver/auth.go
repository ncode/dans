package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/httpapi"
	"github.com/ncode/dans/internal/identifier"
)

// Authenticator resolves current durable state for one API token or browser secret.
type Authenticator interface {
	Authenticate(context.Context, string) (database.Actor, error)
}

// Authentication accepts exactly one header token or browser credential.
// Session creation/deletion handlers establish or clear their own credentials.
func Authentication(authenticator Authenticator) func(http.Handler) http.Handler {
	return (*BrowserSessions)(nil).Authentication(authenticator)
}

// Authentication applies the configured cookie transport without changing token
// validation or cross-origin protection. A nil receiver uses secure cookies.
func (sessions *BrowserSessions) Authentication(authenticator Authenticator) func(http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/livez" || request.URL.Path == "/readyz" {
				next.ServeHTTP(w, request)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			if err := protection.Check(request); err != nil {
				httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindForbidden, err))
				return
			}
			if request.URL.Path == "/api/v1/dans/session" && (request.Method == http.MethodPost || request.Method == http.MethodDelete) {
				next.ServeHTTP(w, request)
				return
			}
			credential, err := sessions.requestCredential(request)
			if err != nil {
				httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindUnauthenticated, database.ErrUnauthenticated))
				return
			}
			if authenticator == nil {
				httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindUnavailable, errors.New("authentication dependency is not configured")))
				return
			}
			actor, err := authenticator.Authenticate(request.Context(), credential)
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

const browserSessionCookie = "__Host-dans_session"

// requestCredential validates the transport as well as the secret format.
func (sessions *BrowserSessions) requestCredential(request *http.Request) (string, error) {
	headers := request.Header.Values("X-API-Key")
	cookies := sessions.credentials(request)
	if len(headers) == 1 && len(cookies) == 0 && identifier.ValidateToken(headers[0]) == nil {
		return headers[0], nil
	}
	if len(headers) == 0 && len(cookies) == 1 && database.ValidBrowserSession(cookies[0]) {
		return cookies[0], nil
	}
	return "", database.ErrUnauthenticated
}

// Keep malformed/duplicate session cookies visible instead of letting net/http
// discard them and accidentally fall back to a supplied header credential.
func (sessions *BrowserSessions) credentials(request *http.Request) []string {
	var values []string
	name := sessions.cookie().Name
	for _, header := range request.Header.Values("Cookie") {
		for part := range strings.SplitSeq(header, ";") {
			key, value, _ := strings.Cut(strings.TrimSpace(part), "=")
			if strings.TrimSpace(key) == name {
				values = append(values, value)
			}
		}
	}
	return values
}
