package httpserver

import (
	"context"
	"net/http"
	"time"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/upstream"
)

// zoneDeletion executes a durably prepared deletion and records its outcome
// before the caller consumes or closes the response body.
type zoneDeletion struct {
	upstream      *upstream.Client
	recordOutcome func(context.Context, database.ZoneDeletionPlan, database.ZoneDeletionOutcomeInput) (database.DNSOutcome, error)
	timeout       time.Duration
	auditFailures AuditFailureReporter
}

func (deletion zoneDeletion) execute(ctx context.Context, serverID api.ServerId, plan database.ZoneDeletionPlan) (*http.Response, database.ZoneDeletionResult, error) {
	metadata := AccessMetadataFromContext(ctx)
	metadata.SetOperationID(plan.Intent.OperationID)
	response, err := deletion.upstream.Forward(func(client api.ClientInterface) (*http.Response, error) {
		return client.DeleteZone(ctx, serverID, api.ZoneId(plan.PowerDNSZoneID))
	})
	input := database.ZoneDeletionOutcomeInput{Result: database.ZoneDeletionUnknown, ResponseClass: "transport_error"}
	if err != nil {
		metadata.SetUpstreamOutcome("transport_error")
	} else {
		metadata.SetUpstreamOutcome("response")
		input.Result = database.ZoneDeletionFailed
		if response.StatusCode == http.StatusNotFound {
			input.Result = database.ZoneDeletionNotFound
		} else if response.StatusCode >= 200 && response.StatusCode < 300 {
			input.Result = database.ZoneDeletionDeleted
		}
		_, input.ResponseClass = classifyMutationResponse(response.StatusCode)
		input.ResponseCode = &response.StatusCode
	}

	// A received status determines the outcome; body reads and request
	// cancellation must not turn it into an unknown result.
	outcomeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deletion.timeout)
	defer cancel()
	if _, outcomeErr := deletion.recordOutcome(outcomeCtx, plan, input); outcomeErr != nil && deletion.auditFailures != nil {
		deletion.auditFailures.ReportAuditFailure()
	}
	return response, input.Result, err
}
