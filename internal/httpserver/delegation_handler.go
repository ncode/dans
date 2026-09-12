package httpserver

import (
	"context"
	"errors"
	"net/url"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
	delegationdomain "github.com/ncode/dans/internal/delegation"
)

type DelegationStore interface {
	CreateDelegation(context.Context, database.Actor, database.DelegationCreate) (database.DelegationDetails, error)
	GetDelegation(context.Context, database.Actor, string) (database.DelegationDetails, error)
	ListDelegations(context.Context, database.Actor, database.DelegationListOptions) (database.DelegationPage, error)
	ListCurrentIdentityDelegations(context.Context, database.Actor, database.DelegationListOptions) (database.DelegationPage, error)
	RevokeDelegation(context.Context, database.Actor, string) error
}

// DelegationHandler overrides delegation operations and delegates every other
// generated strict operation to the next handler.
type DelegationHandler struct {
	api.StrictServerInterface
	store DelegationStore
}

func NewDelegationHandler(store DelegationStore, next api.StrictServerInterface) (*DelegationHandler, error) {
	if store == nil || next == nil {
		return nil, errors.New("delegation HTTP handler: missing dependency")
	}
	return &DelegationHandler{StrictServerInterface: next, store: store}, nil
}

func (handler *DelegationHandler) ListDelegations(ctx context.Context, request api.ListDelegationsRequestObject) (api.ListDelegationsResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return listDelegationsFailure(err), nil
	}
	options, filters, err := delegationListOptions(request.Params)
	if err != nil {
		return listDelegationsFailure(err), nil
	}
	result, err := handler.store.ListDelegations(ctx, actor, options)
	if err != nil {
		return listDelegationsFailure(err), nil
	}
	response, err := delegationPageResponse(result, "delegations", filters)
	if err != nil {
		return nil, err
	}
	return api.ListDelegations200JSONResponse(response), nil
}

func (handler *DelegationHandler) CreateDelegation(ctx context.Context, request api.CreateDelegationRequestObject) (api.CreateDelegationResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil || request.Body == nil {
		if err == nil {
			err = database.ErrInvalid
		}
		return createDelegationFailure(err), nil
	}
	input := database.DelegationCreate{
		ZoneBindingID: request.Body.ZoneBindingId.String(),
		Selectors:     make([]database.DelegationSelectorInput, len(request.Body.Selectors)),
		RecordTypes:   request.Body.RecordTypes,
	}
	input.GranteeIdentityID = nullableResourceID(request.Body.IdentityId)
	input.GranteeGroupID = nullableResourceID(request.Body.GroupId)
	for index, selector := range request.Body.Selectors {
		input.Selectors[index] = database.DelegationSelectorInput{Kind: delegationdomain.SelectorKind(selector.Kind), Value: selector.Value}
	}
	if request.Body.ChangeKinds != nil {
		values := make([]string, len(*request.Body.ChangeKinds))
		for index, kind := range *request.Body.ChangeKinds {
			values[index] = string(kind)
		}
		input.ChangeKinds = &values
	}
	created, err := handler.store.CreateDelegation(ctx, actor, input)
	if err != nil {
		return createDelegationFailure(err), nil
	}
	response, err := delegationResponse(created)
	if err != nil {
		return nil, err
	}
	location := "/api/v1/dans/delegations/" + created.ID
	AccessMetadataFromContext(ctx).SetResourceID(created.ID)
	return api.CreateDelegation201JSONResponse{Body: response, Headers: api.CreateDelegation201ResponseHeaders{Location: &location}}, nil
}

func (handler *DelegationHandler) GetDelegation(ctx context.Context, request api.GetDelegationRequestObject) (api.GetDelegationResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return getDelegationFailure(err), nil
	}
	result, err := handler.store.GetDelegation(ctx, actor, request.DelegationId.String())
	if err != nil {
		return getDelegationFailure(err), nil
	}
	response, err := delegationResponse(result)
	if err != nil {
		return nil, err
	}
	AccessMetadataFromContext(ctx).SetResourceID(result.ID)
	return api.GetDelegation200JSONResponse(response), nil
}

func (handler *DelegationHandler) RevokeDelegation(ctx context.Context, request api.RevokeDelegationRequestObject) (api.RevokeDelegationResponseObject, error) {
	actor, err := requestActor(ctx)
	if err == nil {
		err = handler.store.RevokeDelegation(ctx, actor, request.DelegationId.String())
	}
	if err != nil {
		body, status := managementError(err)
		return api.RevokeDelegationdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	AccessMetadataFromContext(ctx).SetResourceID(request.DelegationId.String())
	return api.RevokeDelegation204Response{}, nil
}

func (handler *DelegationHandler) ListCurrentIdentityDelegations(ctx context.Context, request api.ListCurrentIdentityDelegationsRequestObject) (api.ListCurrentIdentityDelegationsResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return listCurrentDelegationsFailure(err), nil
	}
	filters := url.Values{"identity_id": {actor.IdentityID}}.Encode()
	after, err := decodePageCursor(request.Params.Cursor, "identity-delegations", filters)
	if err != nil {
		return listCurrentDelegationsFailure(err), nil
	}
	result, err := handler.store.ListCurrentIdentityDelegations(ctx, actor, database.DelegationListOptions{
		Limit: intValue(request.Params.Limit), After: after,
	})
	if err != nil {
		return listCurrentDelegationsFailure(err), nil
	}
	items := make([]api.SelfDelegation, len(result.Items))
	for i, item := range result.Items {
		base, err := delegationResponse(item)
		if err != nil {
			return nil, err
		}
		kinds := make([]api.SelfDelegationChangeKinds, len(base.ChangeKinds))
		for j, kind := range base.ChangeKinds {
			kinds[j] = api.SelfDelegationChangeKinds(kind)
		}
		items[i] = api.SelfDelegation{Id: base.Id, ZoneBindingId: base.ZoneBindingId, ZoneId: item.ZoneID, ZoneName: item.ZoneName, GranteeId: base.GranteeId, GranteeKind: api.SelfDelegationGranteeKind(base.GranteeKind), Selectors: base.Selectors, RecordTypes: base.RecordTypes, ChangeKinds: kinds, CreatedAt: base.CreatedAt, RevokedAt: base.RevokedAt}
	}
	next, err := nextCursor(result.Next, "identity-delegations", filters)
	response := api.SelfDelegationPage{Items: items, NextCursor: next}
	if err != nil {
		return nil, err
	}
	return api.ListCurrentIdentityDelegations200JSONResponse(response), nil
}

func delegationListOptions(params api.ListDelegationsParams) (database.DelegationListOptions, string, error) {
	values := make(url.Values)
	options := database.DelegationListOptions{Limit: intValue(params.Limit), Active: params.Active}
	if params.ZoneBindingId != nil {
		value := params.ZoneBindingId.String()
		options.ZoneBindingID = &value
		values.Set("zone_binding_id", value)
	}
	if params.GranteeId != nil {
		value := params.GranteeId.String()
		options.GranteeID = &value
		values.Set("grantee_id", value)
	}
	setBoolFilter(values, "active", params.Active)
	filters := values.Encode()
	after, err := decodePageCursor(params.Cursor, "delegations", filters)
	options.After = after
	return options, filters, err
}

func delegationPageResponse(input database.DelegationPage, resource, filters string) (api.DelegationPage, error) {
	items := make([]api.Delegation, len(input.Items))
	for index := range input.Items {
		item, err := delegationResponse(input.Items[index])
		if err != nil {
			return api.DelegationPage{}, err
		}
		items[index] = item
	}
	next, err := nextCursor(input.Next, resource, filters)
	return api.DelegationPage{Items: items, NextCursor: next}, err
}

func delegationResponse(input database.DelegationDetails) (api.Delegation, error) {
	id, err := uuid.Parse(input.ID)
	if err != nil {
		return api.Delegation{}, err
	}
	bindingID, err := uuid.Parse(input.ZoneBindingID)
	if err != nil {
		return api.Delegation{}, err
	}
	granteeID, err := uuid.Parse(input.GranteeID)
	if err != nil {
		return api.Delegation{}, err
	}
	selectors := make([]api.Selector, len(input.Selectors))
	for index, selector := range input.Selectors {
		selectors[index] = api.Selector{Kind: api.SelectorKind(selector.Kind), Value: selector.Value}
	}
	recordTypes := append([]string(nil), input.RecordTypes...)
	changeKinds := make([]api.DelegationChangeKinds, len(input.ChangeKinds))
	for index, kind := range input.ChangeKinds {
		changeKinds[index] = api.DelegationChangeKinds(kind)
	}
	return api.Delegation{
		Id: id, ZoneBindingId: bindingID, GranteeKind: api.DelegationGranteeKind(input.GranteeKind), GranteeId: granteeID,
		Selectors: selectors, RecordTypes: recordTypes, ChangeKinds: changeKinds, CreatedAt: input.CreatedAt.Time,
		RevokedAt: nullableTimestamp(input.RevokedAt.Valid, input.RevokedAt.Time),
	}, nil
}

func nullableResourceID(value nullable.Nullable[api.NullableResourceID]) *string {
	actual, err := value.Get()
	if err != nil {
		return nil
	}
	converted := actual.String()
	return &converted
}

func listDelegationsFailure(err error) api.ListDelegationsdefaultJSONResponse {
	body, status := managementError(err)
	return api.ListDelegationsdefaultJSONResponse{Body: body, StatusCode: status}
}

func createDelegationFailure(err error) api.CreateDelegationdefaultJSONResponse {
	body, status := managementError(err)
	return api.CreateDelegationdefaultJSONResponse{Body: body, StatusCode: status}
}

func getDelegationFailure(err error) api.GetDelegationdefaultJSONResponse {
	body, status := managementError(err)
	return api.GetDelegationdefaultJSONResponse{Body: body, StatusCode: status}
}

func listCurrentDelegationsFailure(err error) api.ListCurrentIdentityDelegationsdefaultJSONResponse {
	body, status := managementError(err)
	return api.ListCurrentIdentityDelegationsdefaultJSONResponse{Body: body, StatusCode: status}
}

var _ api.StrictServerInterface = (*DelegationHandler)(nil)
