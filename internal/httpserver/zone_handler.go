package httpserver

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/httpapi"
	"github.com/ncode/dans/internal/upstream"
)

// ZoneManagementStore is the persistence boundary for generation-aware zone
// binding and explicit deletion reconciliation.
type ZoneManagementStore interface {
	EnsureZoneBinding(context.Context, database.Actor, database.ZoneBindingInput) (database.ZoneBinding, error)
	GetZoneBinding(context.Context, database.Actor, string) (database.ZoneBinding, error)
	ListZoneBindings(context.Context, database.Actor, database.ZoneBindingListOptions) (database.ZoneBindingPage, error)
	ObserveZoneBinding(context.Context, database.Actor, database.ZoneObservationInput) (database.ZoneObservation, error)
	ConfirmZoneBindingAbsent(context.Context, database.Actor, string) (database.ZoneBinding, error)
	PrepareZoneDeletionRetry(context.Context, database.Actor, database.ZoneDeletionInput) (database.ZoneDeletionPlan, error)
	RecordZoneDeletionOutcome(context.Context, database.ZoneDeletionPlan, database.ZoneDeletionOutcomeInput) (database.DNSOutcome, error)
	RebindZoneBinding(context.Context, database.Actor, string, database.ZoneBindingInput) (database.ZoneBinding, error)
}

// ZoneHandler overrides zone-binding and reconciliation operations and
// delegates the rest of the generated strict interface.
type ZoneHandler struct {
	api.StrictServerInterface
	store         ZoneManagementStore
	upstream      *upstream.Client
	upstreamID    string
	timeout       time.Duration
	auditFailures AuditFailureReporter
}

func NewZoneHandler(store ZoneManagementStore, client *upstream.Client, upstreamID string, timeout time.Duration, auditFailures AuditFailureReporter, next api.StrictServerInterface) (*ZoneHandler, error) {
	if store == nil || client == nil || upstreamID == "" || timeout <= 0 || auditFailures == nil || next == nil {
		return nil, errors.New("zone HTTP handler: missing dependency")
	}
	return &ZoneHandler{StrictServerInterface: next, store: store, upstream: client, upstreamID: upstreamID, timeout: timeout, auditFailures: auditFailures}, nil
}

func (handler *ZoneHandler) ListZoneBindings(ctx context.Context, request api.ListZoneBindingsRequestObject) (api.ListZoneBindingsResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return listZoneBindingsFailure(err), nil
	}
	options, filters, err := zoneBindingListOptions(request.Params)
	if err != nil {
		return listZoneBindingsFailure(err), nil
	}
	result, err := handler.store.ListZoneBindings(ctx, actor, options)
	if err != nil {
		return listZoneBindingsFailure(err), nil
	}
	items := make([]api.ZoneBinding, len(result.Items))
	for index, item := range result.Items {
		items[index], err = zoneBindingResponse(item)
		if err != nil {
			return nil, err
		}
	}
	next, err := nextCursor(result.Next, "zone-bindings", filters)
	if err != nil {
		return nil, err
	}
	return api.ListZoneBindings200JSONResponse{Items: items, NextCursor: next}, nil
}

func (handler *ZoneHandler) CreateZoneBinding(ctx context.Context, request api.CreateZoneBindingRequestObject) (api.CreateZoneBindingResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil || request.Body == nil {
		if err == nil {
			err = database.ErrInvalid
		}
		return createZoneBindingFailure(err), nil
	}
	present, observedID, zoneName, err := handler.observeUpstreamZone(ctx, request.Body.ZoneId)
	if err != nil {
		return createZoneBindingFailure(err), nil
	}
	if !present {
		return createZoneBindingFailure(database.ErrNotFound), nil
	}
	binding, err := handler.store.EnsureZoneBinding(ctx, actor, database.ZoneBindingInput{
		Upstream: handler.upstreamID, PowerDNSZoneID: observedID, ZoneName: zoneName,
	})
	if err != nil {
		return createZoneBindingFailure(err), nil
	}
	response, err := zoneBindingResponse(binding)
	if err != nil {
		return nil, err
	}
	location := "/api/v1/dans/zone-bindings/" + binding.ID
	AccessMetadataFromContext(ctx).SetResourceID(binding.ID)
	return api.CreateZoneBinding201JSONResponse{Body: response, Headers: api.CreateZoneBinding201ResponseHeaders{Location: &location}}, nil
}

func (handler *ZoneHandler) GetZoneBinding(ctx context.Context, request api.GetZoneBindingRequestObject) (api.GetZoneBindingResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return getZoneBindingFailure(err), nil
	}
	binding, err := handler.store.GetZoneBinding(ctx, actor, request.ZoneBindingId.String())
	if err != nil {
		return getZoneBindingFailure(err), nil
	}
	response, err := zoneBindingResponse(binding)
	if err != nil {
		return nil, err
	}
	AccessMetadataFromContext(ctx).SetResourceID(binding.ID)
	return api.GetZoneBinding200JSONResponse(response), nil
}

func (handler *ZoneHandler) ObserveZoneBinding(ctx context.Context, request api.ObserveZoneBindingRequestObject) (api.ObserveZoneBindingResponseObject, error) {
	actor, binding, err := handler.bindingForAction(ctx, request.ZoneBindingId.String())
	if err != nil {
		return observeZoneBindingFailure(err), nil
	}
	present, observedID, _, err := handler.observeUpstreamZone(ctx, binding.PowerDNSZoneID)
	if err != nil {
		return observeZoneBindingFailure(err), nil
	}
	input := database.ZoneObservationInput{BindingID: binding.ID, ZonePresent: present}
	if present {
		input.ObservedZoneID = &observedID
	}
	observation, err := handler.store.ObserveZoneBinding(ctx, actor, input)
	if err != nil {
		return observeZoneBindingFailure(err), nil
	}
	response, err := zoneObservationResponse(observation)
	if err != nil {
		return nil, err
	}
	AccessMetadataFromContext(ctx).SetResourceID(binding.ID)
	return api.ObserveZoneBinding200JSONResponse(response), nil
}

func (handler *ZoneHandler) ConfirmZoneBindingAbsent(ctx context.Context, request api.ConfirmZoneBindingAbsentRequestObject) (api.ConfirmZoneBindingAbsentResponseObject, error) {
	actor, binding, err := handler.bindingForAction(ctx, request.ZoneBindingId.String())
	if err != nil {
		return confirmZoneBindingAbsentFailure(err), nil
	}
	present, _, _, err := handler.observeUpstreamZone(ctx, binding.PowerDNSZoneID)
	if err != nil {
		return confirmZoneBindingAbsentFailure(err), nil
	}
	if present {
		return confirmZoneBindingAbsentFailure(database.ErrConflict), nil
	}
	confirmed, err := handler.store.ConfirmZoneBindingAbsent(ctx, actor, binding.ID)
	if err != nil {
		return confirmZoneBindingAbsentFailure(err), nil
	}
	response, err := zoneBindingResponse(confirmed)
	if err != nil {
		return nil, err
	}
	AccessMetadataFromContext(ctx).SetResourceID(binding.ID)
	return api.ConfirmZoneBindingAbsent200JSONResponse(response), nil
}

func (handler *ZoneHandler) RebindZoneBinding(ctx context.Context, request api.RebindZoneBindingRequestObject) (api.RebindZoneBindingResponseObject, error) {
	actor, binding, err := handler.bindingForAction(ctx, request.ZoneBindingId.String())
	if err != nil || request.Body == nil {
		if err == nil {
			err = database.ErrInvalid
		}
		return rebindZoneBindingFailure(err), nil
	}
	present, observedID, zoneName, err := handler.observeUpstreamZone(ctx, request.Body.ZoneId)
	if err != nil {
		return rebindZoneBindingFailure(err), nil
	}
	if !present {
		return rebindZoneBindingFailure(database.ErrNotFound), nil
	}
	rebound, err := handler.store.RebindZoneBinding(ctx, actor, binding.ID, database.ZoneBindingInput{
		Upstream: handler.upstreamID, PowerDNSZoneID: observedID, ZoneName: zoneName,
	})
	if err != nil {
		return rebindZoneBindingFailure(err), nil
	}
	response, err := zoneBindingResponse(rebound)
	if err != nil {
		return nil, err
	}
	location := "/api/v1/dans/zone-bindings/" + rebound.ID
	AccessMetadataFromContext(ctx).SetResourceID(rebound.ID)
	return api.RebindZoneBinding201JSONResponse{Body: response, Headers: api.RebindZoneBinding201ResponseHeaders{Location: &location}}, nil
}

func (handler *ZoneHandler) RetryZoneBindingDeletion(ctx context.Context, request api.RetryZoneBindingDeletionRequestObject) (api.RetryZoneBindingDeletionResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return retryZoneBindingDeletionFailure(err), nil
	}
	digest := sha256.Sum256(nil)
	plan, err := handler.store.PrepareZoneDeletionRetry(ctx, actor, database.ZoneDeletionInput{
		BindingID: request.ZoneBindingId.String(), RequestDigest: digest[:], Deadline: mutationDeadline(ctx, handler.timeout),
	})
	if err != nil {
		return retryZoneBindingDeletionFailure(err), nil
	}
	AccessMetadataFromContext(ctx).SetOperationID(plan.Intent.OperationID)
	response, err := handler.upstream.Forward(func(client api.ClientInterface) (*http.Response, error) {
		return client.DeleteZone(ctx, api.ServerId("localhost"), api.ZoneId(plan.PowerDNSZoneID))
	})
	if err != nil {
		handler.recordDeletionOutcome(ctx, plan, database.ZoneDeletionOutcomeInput{Result: database.ZoneDeletionUnknown, ResponseClass: "transport_error"})
		return retryZoneBindingDeletionFailure(err), nil
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, upstream.MaxProbeBytes+1))
	_ = response.Body.Close()
	result := database.ZoneDeletionFailed
	if response.StatusCode == http.StatusNotFound {
		result = database.ZoneDeletionNotFound
	} else if response.StatusCode >= 200 && response.StatusCode < 300 {
		result = database.ZoneDeletionDeleted
	}
	if readErr != nil || len(body) > upstream.MaxProbeBytes {
		result = database.ZoneDeletionUnknown
	}
	_, responseClass := classifyMutationResponse(response.StatusCode)
	responseDigest := sha256.Sum256(body)
	handler.recordDeletionOutcome(ctx, plan, database.ZoneDeletionOutcomeInput{
		Result: result, ResponseClass: responseClass, ResponseCode: &response.StatusCode, ResponseDigest: responseDigest[:],
	})
	if result == database.ZoneDeletionDeleted || result == database.ZoneDeletionNotFound {
		AccessMetadataFromContext(ctx).SetResourceID(plan.Binding.ID)
		return api.RetryZoneBindingDeletion204Response{}, nil
	}
	return retryZoneBindingDeletionFailure(httpapi.NewError(httpapi.KindBadGateway, errors.New("PowerDNS rejected deletion retry"))), nil
}

func (handler *ZoneHandler) bindingForAction(ctx context.Context, id string) (database.Actor, database.ZoneBinding, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return database.Actor{}, database.ZoneBinding{}, err
	}
	binding, err := handler.store.GetZoneBinding(ctx, actor, id)
	return actor, binding, err
}

func (handler *ZoneHandler) observeUpstreamZone(ctx context.Context, zoneID string) (bool, string, string, error) {
	withoutRRsets := false
	response, err := handler.upstream.Forward(func(client api.ClientInterface) (*http.Response, error) {
		return client.ListZone(ctx, api.ServerId("localhost"), api.ZoneId(zoneID), &api.ListZoneParams{Rrsets: &withoutRRsets})
	})
	if err != nil {
		return false, "", "", err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return false, "", "", nil
	}
	if response.StatusCode != http.StatusOK {
		return false, "", "", httpapi.NewError(httpapi.KindBadGateway, fmt.Errorf("PowerDNS zone observation status %d", response.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, upstream.MaxProbeBytes+1))
	if err != nil || len(body) > upstream.MaxProbeBytes {
		return false, "", "", httpapi.NewError(httpapi.KindBadGateway, errors.New("PowerDNS zone observation body is invalid"))
	}
	var zone api.Zone
	if err := json.Unmarshal(body, &zone); err != nil || zone.Id == nil || zone.Name == nil || *zone.Id == "" || *zone.Name == "" {
		return false, "", "", httpapi.NewError(httpapi.KindBadGateway, errors.New("PowerDNS zone observation is invalid"))
	}
	return true, *zone.Id, *zone.Name, nil
}

func (handler *ZoneHandler) recordDeletionOutcome(parent context.Context, plan database.ZoneDeletionPlan, input database.ZoneDeletionOutcomeInput) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), handler.timeout)
	defer cancel()
	if _, err := handler.store.RecordZoneDeletionOutcome(ctx, plan, input); err != nil {
		handler.auditFailures.ReportAuditFailure()
	}
}

func zoneBindingListOptions(params api.ListZoneBindingsParams) (database.ZoneBindingListOptions, string, error) {
	values := make(url.Values)
	options := database.ZoneBindingListOptions{Limit: intValue(params.Limit), ZoneName: params.ZoneName}
	if params.Status != nil {
		value := string(*params.Status)
		options.Status = &value
		values.Set("status", value)
	}
	setStringFilter(values, "zone_name", params.ZoneName)
	filters := values.Encode()
	after, err := decodePageCursor(params.Cursor, "zone-bindings", filters)
	options.After = after
	return options, filters, err
}

func zoneBindingResponse(input database.ZoneBinding) (api.ZoneBinding, error) {
	id, err := uuid.Parse(input.ID)
	if err != nil {
		return api.ZoneBinding{}, err
	}
	status := api.ZoneBindingStatus("active")
	if input.RetiredAt.Valid {
		status = api.ZoneBindingStatus("retired")
	}
	return api.ZoneBinding{
		Id: api.ResourceID(id), Generation: int(input.Generation), Upstream: api.ZoneBindingUpstream(input.Upstream),
		ZoneId: input.PowerDNSZoneID, ZoneName: input.ZoneName, Status: status,
		CreatedAt: input.CreatedAt.Time, RetiredAt: nullableTimestamp(input.RetiredAt.Valid, input.RetiredAt.Time),
	}, nil
}

func zoneObservationResponse(input database.ZoneObservation) (api.ZoneObservation, error) {
	id, err := uuid.Parse(input.BindingID)
	if err != nil {
		return api.ZoneObservation{}, err
	}
	observedID := nullable.NewNullNullable[string]()
	if input.ObservedZoneID != nil {
		observedID = nullable.NewNullableWithValue(*input.ObservedZoneID)
	}
	return api.ZoneObservation{
		BindingId: api.ResourceID(id), ObservedAt: input.ObservedAt,
		ZonePresent: input.ZonePresent, ObservedZoneId: observedID,
	}, nil
}

func listZoneBindingsFailure(err error) api.ListZoneBindingsdefaultJSONResponse {
	body, status := managementError(err)
	return api.ListZoneBindingsdefaultJSONResponse{Body: body, StatusCode: status}
}
func createZoneBindingFailure(err error) api.CreateZoneBindingdefaultJSONResponse {
	body, status := strictAPIError(err)
	return api.CreateZoneBindingdefaultJSONResponse{Body: body, StatusCode: status}
}
func getZoneBindingFailure(err error) api.GetZoneBindingdefaultJSONResponse {
	body, status := managementError(err)
	return api.GetZoneBindingdefaultJSONResponse{Body: body, StatusCode: status}
}
func observeZoneBindingFailure(err error) api.ObserveZoneBindingdefaultJSONResponse {
	body, status := strictAPIError(err)
	return api.ObserveZoneBindingdefaultJSONResponse{Body: body, StatusCode: status}
}
func confirmZoneBindingAbsentFailure(err error) api.ConfirmZoneBindingAbsentdefaultJSONResponse {
	body, status := strictAPIError(err)
	return api.ConfirmZoneBindingAbsentdefaultJSONResponse{Body: body, StatusCode: status}
}
func rebindZoneBindingFailure(err error) api.RebindZoneBindingdefaultJSONResponse {
	body, status := strictAPIError(err)
	return api.RebindZoneBindingdefaultJSONResponse{Body: body, StatusCode: status}
}
func retryZoneBindingDeletionFailure(err error) api.RetryZoneBindingDeletiondefaultJSONResponse {
	body, status := strictAPIError(err)
	return api.RetryZoneBindingDeletiondefaultJSONResponse{Body: body, StatusCode: status}
}

func strictAPIError(err error) (api.Error, int) {
	if httpapi.StatusCode(err) == http.StatusInternalServerError {
		return managementError(err)
	}
	body := httpapi.ErrorBody(err)
	response := api.Error{Error: body.Error}
	if len(body.Errors) != 0 {
		response.Errors = &body.Errors
	}
	return response, httpapi.StatusCode(err)
}
