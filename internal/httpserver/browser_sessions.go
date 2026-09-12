package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/httpapi"
	"github.com/ncode/dans/internal/identifier"
)

// BrowserSessionStore persists opaque session digests against current API tokens.
type BrowserSessionStore interface {
	CreateBrowserSession(context.Context, string, string) (database.CreatedBrowserSession, error)
	DeleteBrowserSession(context.Context, string) error
}

// BrowserSessions handles cookie transport separately from generated JSON bodies.
type BrowserSessions struct {
	Store           BrowserSessionStore
	DevelopmentHTTP bool
}

func (h *BrowserSessions) cookie() http.Cookie {
	cookie := http.Cookie{Name: browserSessionCookie, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode}
	if h != nil && h.DevelopmentHTTP {
		cookie.Name = "dans_dev_session"
		cookie.Secure = false
	}
	return cookie
}

func (h *BrowserSessions) CreateBrowserSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		Token string `json:"token"`
	}
	cookies := h.credentials(r)
	if len(r.Header.Values("X-API-Key")) != 0 || len(cookies) > 1 {
		writeSessionError(w, r, database.ErrUnauthenticated)
		return
	}
	if err := httpapi.StrictJSON(r.Body, 4096, func(value struct {
		Token string `json:"token"`
	}) error {
		input = value
		return nil
	}); err != nil || identifier.ValidateToken(input.Token) != nil {
		writeSessionError(w, r, database.ErrUnauthenticated)
		return
	}
	if h.Store == nil {
		writeSessionError(w, r, errors.New("browser session dependency is not configured"))
		return
	}
	previous := ""
	if len(cookies) == 1 {
		previous = cookies[0]
	}
	session, err := h.Store.CreateBrowserSession(r.Context(), input.Token, previous)
	if err != nil {
		writeSessionError(w, r, err)
		return
	}
	body, err := json.Marshal(struct {
		ExpiresAt time.Time `json:"expires_at"`
	}{session.ExpiresAt})
	if err != nil {
		writeSessionError(w, r, err)
		return
	}
	cookie := h.cookie()
	cookie.Value = session.Secret
	cookie.Expires = session.ExpiresAt
	cookie.MaxAge = max(1, min(604800, int(time.Until(session.ExpiresAt).Seconds())))
	http.SetCookie(w, &cookie)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(body) // The response is committed; disconnects cannot change session state.
}

func (h *BrowserSessions) DeleteBrowserSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	cookie := h.cookie()
	cookie.Expires = time.Unix(1, 0).UTC()
	cookie.MaxAge = -1
	http.SetCookie(w, &cookie)
	cookies := h.credentials(r)
	if len(r.Header.Values("X-API-Key")) != 0 || len(cookies) > 1 {
		writeSessionError(w, r, database.ErrUnauthenticated)
		return
	}
	if len(cookies) == 1 && database.ValidBrowserSession(cookies[0]) {
		if h.Store == nil {
			writeSessionError(w, r, errors.New("browser session dependency is not configured"))
			return
		}
		if err := h.Store.DeleteBrowserSession(r.Context(), cookies[0]); err != nil {
			writeSessionError(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeSessionError(w http.ResponseWriter, r *http.Request, err error) {
	kind := httpapi.KindUnavailable
	if errors.Is(err, database.ErrUnauthenticated) {
		kind = httpapi.KindUnauthenticated
	}
	httpapi.WriteError(w, RequestIDFromContext(r.Context()), httpapi.NewError(kind, err))
}

// CreateBrowserSession handles the local sign-in operation, never upstream.
func (proxy *PowerDNSProxy) CreateBrowserSession(w http.ResponseWriter, r *http.Request) {
	if proxy.BrowserSessions == nil {
		writeSessionError(w, r, errors.New("browser session handler is not configured"))
		return
	}
	proxy.BrowserSessions.CreateBrowserSession(w, r)
}

// DeleteBrowserSession handles the local sign-out operation, never upstream.
func (proxy *PowerDNSProxy) DeleteBrowserSession(w http.ResponseWriter, r *http.Request) {
	if proxy.BrowserSessions == nil {
		writeSessionError(w, r, errors.New("browser session handler is not configured"))
		return
	}
	proxy.BrowserSessions.DeleteBrowserSession(w, r)
}
