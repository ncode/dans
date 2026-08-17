package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
)

// NewRootHandler registers the three operations whose OpenAPI server override
// places them outside the global /api/v1 compatibility base.
func NewRootHandler(health *Health, contractJSON []byte, API http.Handler) (http.Handler, error) {
	if health == nil || API == nil || !json.Valid(contractJSON) {
		return nil, errors.New("root routes: invalid configuration")
	}
	document := append([]byte(nil), contractJSON...)
	mux := http.NewServeMux()
	mux.HandleFunc(http.MethodGet+" /livez", health.Livez)
	mux.HandleFunc(http.MethodGet+" /readyz", health.Readyz)
	mux.HandleFunc(http.MethodGet+" /api/docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(document)
	})
	mux.Handle("/", API)
	return mux, nil
}
