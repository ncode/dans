package httpserver

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ncode/dans/api"
	contractdoc "github.com/ncode/dans/api/openapi"
	"github.com/ncode/dans/internal/httpapi"
	"github.com/ncode/dans/internal/upstream"
)

// ApplicationConfig contains the immutable dependencies of the HTTP trust
// boundary. DANS supplies the management operations; PowerDNSProxy overrides
// every upstream operation.
type ApplicationConfig struct {
	Boundary          BoundaryConfig
	Logger            *slog.Logger
	Authenticator     Authenticator
	Schema            Probe
	PowerDNSReady     func() bool
	Health            *Health
	Upstream          *upstream.Client
	DANS              DANSOperations
	Mutations         RRsetMutationStore
	Lifecycle         ZoneLifecycleStore
	Denials           AuthorizationDenialAuditor
	AuditFailures     AuditFailureReporter
	LifecycleFailures LifecycleFailureReporter
	UpstreamID        string
	MutationTimeout   time.Duration
}

// NewApplicationHandler assembles the production middleware and generated
// router from the exact embedded public contract.
func NewApplicationHandler(config ApplicationConfig) (http.Handler, error) {
	if config.Logger == nil || config.Authenticator == nil || config.Schema == nil || config.PowerDNSReady == nil || config.Health == nil || config.Upstream == nil || config.DANS == nil ||
		config.Mutations == nil || config.Lifecycle == nil || config.Denials == nil || config.AuditFailures == nil || config.LifecycleFailures == nil || config.UpstreamID == "" || config.MutationTimeout <= 0 {
		return nil, errors.New("HTTP application: missing dependency")
	}
	boundary, err := NewBoundary(config.Boundary)
	if err != nil {
		return nil, err
	}
	document, err := contractdoc.Spec()
	if err != nil {
		return nil, errors.New("HTTP application: parse embedded contract")
	}
	validation, err := NewContractValidation(document)
	if err != nil {
		return nil, err
	}

	proxy := &PowerDNSProxy{
		DANSOperations: config.DANS, Upstream: config.Upstream,
		Mutations: config.Mutations, Lifecycle: config.Lifecycle, AuditFailures: config.AuditFailures, LifecycleFailures: config.LifecycleFailures,
		UpstreamID: config.UpstreamID, MutationTimeout: config.MutationTimeout,
	}
	generated := api.HandlerWithOptions(proxy, api.StdHTTPServerOptions{
		BaseURL: "/api/v1",
		ErrorHandlerFunc: func(w http.ResponseWriter, request *http.Request, err error) {
			httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindInvalidRequest, err))
		},
	})
	root, err := NewRootHandler(config.Health, contractdoc.JSON(), generated)
	if err != nil {
		return nil, err
	}

	return Chain(
		root,
		boundary,
		AccessLog(config.Logger),
		validation,
		Authentication(config.Authenticator),
		Compatibility(CompatibilityConfig{Schema: config.Schema, PowerDNSCompatible: config.PowerDNSReady}),
		Authorization(config.Denials, config.AuditFailures),
	), nil
}
