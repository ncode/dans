package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
)

// AuditStore is the immutable operator audit read surface.
type AuditStore interface {
	ListAuditEvents(context.Context, database.Actor, database.AuditListOptions) (database.AuditPage, error)
}

// AuditHandler overrides audit operations and delegates the rest of the
// generated strict interface.
type AuditHandler struct {
	api.StrictServerInterface
	store AuditStore
}

func NewAuditHandler(store AuditStore, next api.StrictServerInterface) (*AuditHandler, error) {
	if store == nil || next == nil {
		return nil, errors.New("audit HTTP handler: missing dependency")
	}
	return &AuditHandler{StrictServerInterface: next, store: store}, nil
}

func (handler *AuditHandler) ListAuditEvents(ctx context.Context, request api.ListAuditEventsRequestObject) (api.ListAuditEventsResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return listAuditEventsFailure(err), nil
	}
	options, filters, err := auditListOptions(request.Params)
	if err != nil {
		return listAuditEventsFailure(err), nil
	}
	result, err := handler.store.ListAuditEvents(ctx, actor, options)
	if err != nil {
		return listAuditEventsFailure(err), nil
	}
	items := make([]api.AuditEvent, len(result.Items))
	for index, record := range result.Items {
		items[index], err = auditEventResponse(record)
		if err != nil {
			return nil, err
		}
	}
	next, err := nextCursor(result.Next, "audit-events", filters)
	if err != nil {
		return nil, err
	}
	return api.ListAuditEvents200JSONResponse{Items: items, NextCursor: next}, nil
}

func auditListOptions(params api.ListAuditEventsParams) (database.AuditListOptions, string, error) {
	values := make(url.Values)
	options := database.AuditListOptions{Limit: intValue(params.Limit)}
	if params.ActorId != nil {
		value := params.ActorId.String()
		options.ActorID = &value
		values.Set("actor_id", value)
	}
	for _, filter := range []struct {
		name        string
		value       *string
		destination **string
	}{
		{name: "action", value: params.Action, destination: &options.Action},
		{name: "target_type", value: params.TargetType, destination: &options.TargetKind},
		{name: "target_id", value: params.TargetId, destination: &options.TargetID},
		{name: "result", value: params.Result, destination: &options.Result},
	} {
		if filter.value != nil {
			value := *filter.value
			*filter.destination = &value
			values.Set(filter.name, value)
		}
	}
	filters := values.Encode()
	after, err := decodePageCursor(params.Cursor, "audit-events", filters)
	options.After = after
	return options, filters, err
}

func auditEventResponse(input database.AuditRecord) (api.AuditEvent, error) {
	id, err := uuid.Parse(input.ID)
	if err != nil {
		return api.AuditEvent{}, err
	}
	actorID := nullable.NewNullNullable[api.NullableResourceID]()
	if input.ActorID != nil {
		parsed, err := uuid.Parse(*input.ActorID)
		if err != nil {
			return api.AuditEvent{}, err
		}
		actorID = nullable.NewNullableWithValue(api.NullableResourceID(parsed))
	}
	details := make(map[string]any)
	if len(input.Details) != 0 {
		if err := json.Unmarshal(input.Details, &details); err != nil {
			return api.AuditEvent{}, err
		}
	}
	return api.AuditEvent{
		Id: api.ResourceID(id), OccurredAt: input.OccurredAt, RequestId: input.RequestID,
		ActorId: actorID, Action: input.Action, TargetType: input.TargetKind,
		TargetId: nullable.NewNullableWithValue(api.NullableString(input.TargetID)),
		Result:   input.Result, Details: details,
	}, nil
}

func listAuditEventsFailure(err error) api.ListAuditEventsdefaultJSONResponse {
	body, status := managementError(err)
	return api.ListAuditEventsdefaultJSONResponse{Body: body, StatusCode: status}
}
