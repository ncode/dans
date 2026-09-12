package httpserver

import (
	"context"
	"errors"
	"time"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/upstream"
)

// GeneratedConfig contains the concrete runtime services shared by every DANS
// strict handler.
type GeneratedConfig struct {
	Store           *database.Store
	Upstream        *upstream.Client
	UpstreamID      string
	MutationTimeout time.Duration
	AuditFailures   AuditFailureReporter
}

// NewDANSOperations builds the complete generated DANS management chain. The
// returned standard server is embedded by PowerDNSProxy, which owns every
// compatibility operation explicitly.
func NewDANSOperations(config GeneratedConfig) (DANSOperations, error) {
	if config.Store == nil || config.Upstream == nil || config.UpstreamID == "" || config.MutationTimeout <= 0 || config.AuditFailures == nil {
		return nil, errors.New("generated DANS handlers: missing dependency")
	}
	terminal := &strictRoot{}
	zone, err := NewZoneHandler(config.Store, config.Upstream, config.UpstreamID, config.MutationTimeout, config.AuditFailures, terminal)
	if err != nil {
		return nil, err
	}
	audit, err := NewAuditHandler(config.Store, zone)
	if err != nil {
		return nil, err
	}
	delegations, err := NewDelegationHandler(config.Store, audit)
	if err != nil {
		return nil, err
	}
	iam, err := NewIAMHandler(config.Store, delegations)
	if err != nil {
		return nil, err
	}
	browse, err := NewBrowseHandler(config.Store, config.UpstreamID, iam)
	if err != nil {
		return nil, err
	}
	return &auditExportServer{ServerInterface: NewGeneratedServer(browse), store: config.Store}, nil
}

// strictRoot is unreachable for compatibility operations because
// PowerDNSProxy overrides those methods at the standard-server layer. It owns
// only root operations, which the outer RootHandler normally intercepts.
type strictRoot struct{ api.StrictServerInterface }

func (*strictRoot) GetAPIDocument(context.Context, api.GetAPIDocumentRequestObject) (api.GetAPIDocumentResponseObject, error) {
	return api.GetAPIDocument401JSONResponse{DansUnauthorizedJSONResponse: api.DansUnauthorizedJSONResponse{Error: "unauthorized"}}, nil
}

func (*strictRoot) GetLiveness(context.Context, api.GetLivenessRequestObject) (api.GetLivenessResponseObject, error) {
	return api.GetLiveness503Response{}, nil
}

func (*strictRoot) GetReadiness(context.Context, api.GetReadinessRequestObject) (api.GetReadinessResponseObject, error) {
	return api.GetReadiness503Response{}, nil
}

var _ DANSOperations = NewGeneratedServer(&strictRoot{})
