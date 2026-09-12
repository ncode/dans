package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/httpapi"
	"github.com/ncode/dans/internal/page"
)

// IAMStore is the stable identity, group, membership, and token application
// service consumed by the generated HTTP handlers.
type IAMStore interface {
	CreateIdentity(context.Context, database.Actor, database.IdentityCreate) (database.Identity, error)
	GetIdentity(context.Context, database.Actor, string) (database.Identity, error)
	ListIdentities(context.Context, database.Actor, database.IdentityListOptions) (database.IdentityPage, error)
	PatchIdentity(context.Context, database.Actor, string, database.IdentityPatch) (database.Identity, error)
	CreateGroup(context.Context, database.Actor, database.GroupCreate) (database.Group, error)
	GetGroup(context.Context, database.Actor, string) (database.Group, error)
	ListGroups(context.Context, database.Actor, database.GroupListOptions) (database.GroupPage, error)
	PatchGroup(context.Context, database.Actor, string, database.GroupPatch) (database.Group, error)
	AddGroupMember(context.Context, database.Actor, string, string) (database.GroupMembership, error)
	RemoveGroupMember(context.Context, database.Actor, string, string) error
	ListGroupMembers(context.Context, database.Actor, string, database.IdentityListOptions) (database.IdentityPage, error)
	ListIdentityGroups(context.Context, database.Actor, string, database.GroupListOptions) (database.GroupPage, error)
	ListCurrentIdentityGroups(context.Context, database.Actor, database.GroupListOptions) (database.GroupPage, error)
	GetCurrentIdentity(context.Context, database.Actor) (database.Identity, error)
	CreateToken(context.Context, database.Actor, string, database.TokenCreate) (database.CreatedToken, error)
	ListTokens(context.Context, database.Actor, string, database.TokenListOptions) (database.TokenPage, error)
	RevokeToken(context.Context, database.Actor, string, string) error
}

// IAMHandler overrides IAM operations and delegates every other generated
// strict operation to the next handler.
type IAMHandler struct {
	api.StrictServerInterface
	store IAMStore
}

func NewIAMHandler(store IAMStore, next api.StrictServerInterface) (*IAMHandler, error) {
	if store == nil || next == nil {
		return nil, errors.New("IAM HTTP handler: missing dependency")
	}
	return &IAMHandler{StrictServerInterface: next, store: store}, nil
}

// NewGeneratedServer adapts strict generated handlers with secret-safe errors.
func NewGeneratedServer(strict api.StrictServerInterface) api.ServerInterface {
	return api.NewStrictHandlerWithOptions(strict, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, request *http.Request, err error) {
			httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindInvalidRequest, err))
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, request *http.Request, err error) {
			httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindInternal, err))
		},
	})
}

func (handler *IAMHandler) ListIdentities(ctx context.Context, request api.ListIdentitiesRequestObject) (api.ListIdentitiesResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return listIdentitiesFailure(err), nil
	}
	options, filters, err := identityListOptions(request.Params)
	if err != nil {
		return listIdentitiesFailure(err), nil
	}
	result, err := handler.store.ListIdentities(ctx, actor, options)
	if err != nil {
		return listIdentitiesFailure(err), nil
	}
	response, err := identityPageResponse(result, "identities", filters)
	if err != nil {
		return nil, err
	}
	return api.ListIdentities200JSONResponse(response), nil
}

func (handler *IAMHandler) CreateIdentity(ctx context.Context, request api.CreateIdentityRequestObject) (api.CreateIdentityResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil || request.Body == nil {
		if err == nil {
			err = database.ErrInvalid
		}
		return createIdentityFailure(err), nil
	}
	created, err := handler.store.CreateIdentity(ctx, actor, database.IdentityCreate{
		Kind: string(request.Body.Kind), Handle: request.Body.Handle, DisplayName: inputNullableString(request.Body.DisplayName),
	})
	if err != nil {
		return createIdentityFailure(err), nil
	}
	response, err := identityResponse(created)
	if err != nil {
		return nil, err
	}
	location := "/api/v1/dans/identities/" + created.ID
	AccessMetadataFromContext(ctx).SetResourceID(created.ID)
	return api.CreateIdentity201JSONResponse{Body: response, Headers: api.CreateIdentity201ResponseHeaders{Location: &location}}, nil
}

func (handler *IAMHandler) GetIdentity(ctx context.Context, request api.GetIdentityRequestObject) (api.GetIdentityResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return getIdentityFailure(err), nil
	}
	identity, err := handler.store.GetIdentity(ctx, actor, request.IdentityId.String())
	if err != nil {
		return getIdentityFailure(err), nil
	}
	response, err := identityResponse(identity)
	if err != nil {
		return nil, err
	}
	AccessMetadataFromContext(ctx).SetResourceID(identity.ID)
	return api.GetIdentity200JSONResponse(response), nil
}

func (handler *IAMHandler) UpdateIdentity(ctx context.Context, request api.UpdateIdentityRequestObject) (api.UpdateIdentityResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil || request.Body == nil {
		if err == nil {
			err = database.ErrInvalid
		}
		return updateIdentityFailure(err), nil
	}
	updated, err := handler.store.PatchIdentity(ctx, actor, request.IdentityId.String(), database.IdentityPatch{
		DisplayName: stringPatch(request.Body.DisplayName), Enabled: request.Body.Enabled, Operator: request.Body.Operator,
	})
	if err != nil {
		return updateIdentityFailure(err), nil
	}
	response, err := identityResponse(updated)
	if err != nil {
		return nil, err
	}
	AccessMetadataFromContext(ctx).SetResourceID(updated.ID)
	return api.UpdateIdentity200JSONResponse(response), nil
}

func (handler *IAMHandler) ListGroups(ctx context.Context, request api.ListGroupsRequestObject) (api.ListGroupsResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return listGroupsFailure(err), nil
	}
	options, filters, err := groupListOptions(request.Params, "groups")
	if err != nil {
		return listGroupsFailure(err), nil
	}
	result, err := handler.store.ListGroups(ctx, actor, options)
	if err != nil {
		return listGroupsFailure(err), nil
	}
	response, err := groupPageResponse(result, "groups", filters)
	if err != nil {
		return nil, err
	}
	return api.ListGroups200JSONResponse(response), nil
}

func (handler *IAMHandler) CreateGroup(ctx context.Context, request api.CreateGroupRequestObject) (api.CreateGroupResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil || request.Body == nil {
		if err == nil {
			err = database.ErrInvalid
		}
		return createGroupFailure(err), nil
	}
	created, err := handler.store.CreateGroup(ctx, actor, database.GroupCreate{
		Handle: request.Body.Handle, DisplayName: inputNullableString(request.Body.DisplayName),
	})
	if err != nil {
		return createGroupFailure(err), nil
	}
	response, err := groupResponse(created)
	if err != nil {
		return nil, err
	}
	location := "/api/v1/dans/groups/" + created.ID
	AccessMetadataFromContext(ctx).SetResourceID(created.ID)
	return api.CreateGroup201JSONResponse{Body: response, Headers: api.CreateGroup201ResponseHeaders{Location: &location}}, nil
}

func (handler *IAMHandler) GetGroup(ctx context.Context, request api.GetGroupRequestObject) (api.GetGroupResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return getGroupFailure(err), nil
	}
	group, err := handler.store.GetGroup(ctx, actor, request.GroupId.String())
	if err != nil {
		return getGroupFailure(err), nil
	}
	response, err := groupResponse(group)
	if err != nil {
		return nil, err
	}
	AccessMetadataFromContext(ctx).SetResourceID(group.ID)
	return api.GetGroup200JSONResponse(response), nil
}

func (handler *IAMHandler) UpdateGroup(ctx context.Context, request api.UpdateGroupRequestObject) (api.UpdateGroupResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil || request.Body == nil {
		if err == nil {
			err = database.ErrInvalid
		}
		return updateGroupFailure(err), nil
	}
	updated, err := handler.store.PatchGroup(ctx, actor, request.GroupId.String(), database.GroupPatch{
		DisplayName: stringPatch(request.Body.DisplayName), Enabled: request.Body.Enabled,
	})
	if err != nil {
		return updateGroupFailure(err), nil
	}
	response, err := groupResponse(updated)
	if err != nil {
		return nil, err
	}
	AccessMetadataFromContext(ctx).SetResourceID(updated.ID)
	return api.UpdateGroup200JSONResponse(response), nil
}

func (handler *IAMHandler) ListGroupMembers(ctx context.Context, request api.ListGroupMembersRequestObject) (api.ListGroupMembersResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return listGroupMembersFailure(err), nil
	}
	values := url.Values{"group_id": {request.GroupId.String()}}
	setStringFilter(values, "handle_prefix", request.Params.HandlePrefix)
	filters := values.Encode()
	options, err := identityPageOptions(request.Params.Limit, request.Params.Cursor, "group-members", filters)
	if err != nil {
		return listGroupMembersFailure(err), nil
	}
	options.HandlePrefix = request.Params.HandlePrefix
	result, err := handler.store.ListGroupMembers(ctx, actor, request.GroupId.String(), options)
	if err != nil {
		return listGroupMembersFailure(err), nil
	}
	response, err := identityPageResponse(result, "group-members", filters)
	if err != nil {
		return nil, err
	}
	return api.ListGroupMembers200JSONResponse(response), nil
}

func (handler *IAMHandler) AddGroupMember(ctx context.Context, request api.AddGroupMemberRequestObject) (api.AddGroupMemberResponseObject, error) {
	actor, err := requestActor(ctx)
	if err == nil {
		_, err = handler.store.AddGroupMember(ctx, actor, request.GroupId.String(), request.IdentityId.String())
	}
	if err != nil {
		body, status := managementError(err)
		return api.AddGroupMemberdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	AccessMetadataFromContext(ctx).SetResourceID(request.GroupId.String() + ":" + request.IdentityId.String())
	return api.AddGroupMember204Response{}, nil
}

func (handler *IAMHandler) RemoveGroupMember(ctx context.Context, request api.RemoveGroupMemberRequestObject) (api.RemoveGroupMemberResponseObject, error) {
	actor, err := requestActor(ctx)
	if err == nil {
		err = handler.store.RemoveGroupMember(ctx, actor, request.GroupId.String(), request.IdentityId.String())
	}
	if err != nil {
		body, status := managementError(err)
		return api.RemoveGroupMemberdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	AccessMetadataFromContext(ctx).SetResourceID(request.GroupId.String() + ":" + request.IdentityId.String())
	return api.RemoveGroupMember204Response{}, nil
}

func (handler *IAMHandler) GetCurrentIdentity(ctx context.Context, _ api.GetCurrentIdentityRequestObject) (api.GetCurrentIdentityResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return getCurrentIdentityFailure(err), nil
	}
	identity, err := handler.store.GetCurrentIdentity(ctx, actor)
	if err != nil {
		return getCurrentIdentityFailure(err), nil
	}
	response, err := identityResponse(identity)
	if err != nil {
		return nil, err
	}
	return api.GetCurrentIdentity200JSONResponse(response), nil
}

func (handler *IAMHandler) ListCurrentIdentityGroups(ctx context.Context, request api.ListCurrentIdentityGroupsRequestObject) (api.ListCurrentIdentityGroupsResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return listCurrentGroupsFailure(err), nil
	}
	values := url.Values{"identity_id": {actor.IdentityID}}
	setStringFilter(values, "handle_prefix", request.Params.HandlePrefix)
	filters := values.Encode()
	options, err := currentGroupListOptions(request.Params, filters)
	if err != nil {
		return listCurrentGroupsFailure(err), nil
	}
	result, err := handler.store.ListCurrentIdentityGroups(ctx, actor, options)
	if err != nil {
		return listCurrentGroupsFailure(err), nil
	}
	response, err := groupPageResponse(result, "identity-groups", filters)
	if err != nil {
		return nil, err
	}
	return api.ListCurrentIdentityGroups200JSONResponse(response), nil
}

func (handler *IAMHandler) ListIdentityTokens(ctx context.Context, request api.ListIdentityTokensRequestObject) (api.ListIdentityTokensResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return listIdentityTokensFailure(err), nil
	}
	result, filters, err := handler.listTokens(ctx, actor, request.IdentityId.String(), request.Params.Limit, request.Params.Cursor)
	if err != nil {
		return listIdentityTokensFailure(err), nil
	}
	response, err := tokenPageResponse(result, "tokens", filters)
	if err != nil {
		return nil, err
	}
	return api.ListIdentityTokens200JSONResponse(response), nil
}

func (handler *IAMHandler) CreateIdentityToken(ctx context.Context, request api.CreateIdentityTokenRequestObject) (api.CreateIdentityTokenResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil || request.Body == nil {
		if err == nil {
			err = database.ErrInvalid
		}
		return createIdentityTokenFailure(err), nil
	}
	created, err := handler.createToken(ctx, actor, request.IdentityId.String(), *request.Body)
	if err != nil {
		return createIdentityTokenFailure(err), nil
	}
	response, err := tokenSecretResponse(created)
	if err != nil {
		return nil, err
	}
	location := "/api/v1/dans/identities/" + request.IdentityId.String() + "/tokens/" + created.Token.ID
	AccessMetadataFromContext(ctx).SetResourceID(created.Token.ID)
	return api.CreateIdentityToken201JSONResponse{Body: response, Headers: api.CreateIdentityToken201ResponseHeaders{Location: &location}}, nil
}

func (handler *IAMHandler) RevokeIdentityToken(ctx context.Context, request api.RevokeIdentityTokenRequestObject) (api.RevokeIdentityTokenResponseObject, error) {
	actor, err := requestActor(ctx)
	if err == nil {
		err = handler.store.RevokeToken(ctx, actor, request.IdentityId.String(), request.TokenId.String())
	}
	if err != nil {
		body, status := managementError(err)
		return api.RevokeIdentityTokendefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	AccessMetadataFromContext(ctx).SetResourceID(request.TokenId.String())
	return api.RevokeIdentityToken204Response{}, nil
}

func (handler *IAMHandler) ListCurrentIdentityTokens(ctx context.Context, request api.ListCurrentIdentityTokensRequestObject) (api.ListCurrentIdentityTokensResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return listCurrentTokensFailure(err), nil
	}
	result, filters, err := handler.listTokens(ctx, actor, actor.IdentityID, request.Params.Limit, request.Params.Cursor)
	if err != nil {
		return listCurrentTokensFailure(err), nil
	}
	response, err := tokenPageResponse(result, "tokens", filters)
	if err != nil {
		return nil, err
	}
	return api.ListCurrentIdentityTokens200JSONResponse(response), nil
}

func (handler *IAMHandler) CreateCurrentIdentityToken(ctx context.Context, request api.CreateCurrentIdentityTokenRequestObject) (api.CreateCurrentIdentityTokenResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil || request.Body == nil {
		if err == nil {
			err = database.ErrInvalid
		}
		return createCurrentTokenFailure(err), nil
	}
	created, err := handler.createToken(ctx, actor, actor.IdentityID, *request.Body)
	if err != nil {
		return createCurrentTokenFailure(err), nil
	}
	response, err := tokenSecretResponse(created)
	if err != nil {
		return nil, err
	}
	location := "/api/v1/dans/me/tokens/" + created.Token.ID
	AccessMetadataFromContext(ctx).SetResourceID(created.Token.ID)
	return api.CreateCurrentIdentityToken201JSONResponse{Body: response, Headers: api.CreateCurrentIdentityToken201ResponseHeaders{Location: &location}}, nil
}

func (handler *IAMHandler) RevokeCurrentIdentityToken(ctx context.Context, request api.RevokeCurrentIdentityTokenRequestObject) (api.RevokeCurrentIdentityTokenResponseObject, error) {
	actor, err := requestActor(ctx)
	if err == nil {
		err = handler.store.RevokeToken(ctx, actor, actor.IdentityID, request.TokenId.String())
	}
	if err != nil {
		body, status := managementError(err)
		return api.RevokeCurrentIdentityTokendefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	AccessMetadataFromContext(ctx).SetResourceID(request.TokenId.String())
	return api.RevokeCurrentIdentityToken204Response{}, nil
}

func (handler *IAMHandler) createToken(ctx context.Context, actor database.Actor, identityID string, input api.TokenCreate) (database.CreatedToken, error) {
	var expiresAt *time.Time
	if value, err := input.ExpiresAt.Get(); err == nil {
		converted := time.Time(value)
		expiresAt = &converted
	}
	return handler.store.CreateToken(ctx, actor, identityID, database.TokenCreate{Label: input.Label, ExpiresAt: expiresAt})
}

func (handler *IAMHandler) listTokens(ctx context.Context, actor database.Actor, identityID string, limit *api.Limit, cursor *api.Cursor) (database.TokenPage, string, error) {
	filters := url.Values{"identity_id": {identityID}}.Encode()
	options := database.TokenListOptions{Limit: intValue(limit)}
	after, err := decodePageCursor(cursor, "tokens", filters)
	if err != nil {
		return database.TokenPage{}, filters, err
	}
	options.After = after
	result, err := handler.store.ListTokens(ctx, actor, identityID, options)
	return result, filters, err
}

func requestActor(ctx context.Context) (database.Actor, error) {
	actor, ok := ActorFromContext(ctx)
	if !ok {
		return database.Actor{}, database.ErrUnauthenticated
	}
	actor.RequestID = RequestIDFromContext(ctx)
	if actor.RequestID == "" {
		return database.Actor{}, database.ErrInvalid
	}
	return actor, nil
}

func identityListOptions(params api.ListIdentitiesParams) (database.IdentityListOptions, string, error) {
	values := make(url.Values)
	options := database.IdentityListOptions{Limit: intValue(params.Limit), Enabled: params.Enabled, Operator: params.Operator, Handle: params.Handle, HandlePrefix: params.HandlePrefix}
	if params.Kind != nil {
		kind := string(*params.Kind)
		options.Kind = &kind
		values.Set("kind", kind)
	}
	setBoolFilter(values, "enabled", params.Enabled)
	setBoolFilter(values, "operator", params.Operator)
	setStringFilter(values, "handle", params.Handle)
	setStringFilter(values, "handle_prefix", params.HandlePrefix)
	filters := values.Encode()
	after, err := decodePageCursor(params.Cursor, "identities", filters)
	options.After = after
	return options, filters, err
}

func identityPageOptions(limit *api.Limit, cursor *api.Cursor, resource, filters string) (database.IdentityListOptions, error) {
	after, err := decodePageCursor(cursor, resource, filters)
	return database.IdentityListOptions{Limit: intValue(limit), After: after}, err
}

func groupListOptions(params api.ListGroupsParams, resource string) (database.GroupListOptions, string, error) {
	values := make(url.Values)
	setBoolFilter(values, "enabled", params.Enabled)
	setStringFilter(values, "handle", params.Handle)
	setStringFilter(values, "handle_prefix", params.HandlePrefix)
	filters := values.Encode()
	after, err := decodePageCursor(params.Cursor, resource, filters)
	return database.GroupListOptions{Limit: intValue(params.Limit), After: after, Enabled: params.Enabled, Handle: params.Handle, HandlePrefix: params.HandlePrefix}, filters, err
}

func currentGroupListOptions(params api.ListCurrentIdentityGroupsParams, filters string) (database.GroupListOptions, error) {
	after, err := decodePageCursor(params.Cursor, "identity-groups", filters)
	return database.GroupListOptions{Limit: intValue(params.Limit), After: after, HandlePrefix: params.HandlePrefix}, err
}

func decodePageCursor(cursor *api.Cursor, resource, filters string) (*page.Key, error) {
	if cursor == nil {
		return nil, nil
	}
	key, err := page.DecodeCursor(string(*cursor), resource, filters)
	if err != nil {
		return nil, database.ErrInvalid
	}
	return &key, nil
}

func intValue(value *api.Limit) int {
	if value == nil {
		return 0
	}
	return int(*value)
}

func inputNullableString(value nullable.Nullable[api.NullableString]) *string {
	actual, err := value.Get()
	if err != nil {
		return nil
	}
	converted := string(actual)
	return &converted
}

func stringPatch(value nullable.Nullable[string]) database.NullableStringPatch {
	patch := database.NullableStringPatch{Set: value.IsSpecified()}
	if actual, err := value.Get(); err == nil {
		patch.Value = &actual
	}
	return patch
}

func identityPageResponse(input database.IdentityPage, resource, filters string) (api.IdentityPage, error) {
	items := make([]api.Identity, len(input.Items))
	for index := range input.Items {
		item, err := identityResponse(input.Items[index])
		if err != nil {
			return api.IdentityPage{}, err
		}
		items[index] = item
	}
	next, err := nextCursor(input.Next, resource, filters)
	return api.IdentityPage{Items: items, NextCursor: next}, err
}

func groupPageResponse(input database.GroupPage, resource, filters string) (api.GroupPage, error) {
	items := make([]api.Group, len(input.Items))
	for index := range input.Items {
		item, err := groupResponse(input.Items[index])
		if err != nil {
			return api.GroupPage{}, err
		}
		items[index] = item
	}
	next, err := nextCursor(input.Next, resource, filters)
	return api.GroupPage{Items: items, NextCursor: next}, err
}

func tokenPageResponse(input database.TokenPage, resource, filters string) (api.TokenPage, error) {
	items := make([]api.Token, len(input.Items))
	for index := range input.Items {
		item, err := tokenResponse(input.Items[index])
		if err != nil {
			return api.TokenPage{}, err
		}
		items[index] = item
	}
	next, err := nextCursor(input.Next, resource, filters)
	return api.TokenPage{Items: items, NextCursor: next}, err
}

func identityResponse(input database.Identity) (api.Identity, error) {
	id, err := uuid.Parse(input.ID)
	if err != nil {
		return api.Identity{}, err
	}
	return api.Identity{
		Id: id, Kind: api.IdentityKind(input.Kind), Handle: input.Handle, DisplayName: nullableString(input.DisplayName),
		Enabled: input.Enabled, Operator: input.IsOperator, CreatedAt: input.CreatedAt.Time, UpdatedAt: input.UpdatedAt.Time,
	}, nil
}

func groupResponse(input database.Group) (api.Group, error) {
	id, err := uuid.Parse(input.ID)
	if err != nil {
		return api.Group{}, err
	}
	return api.Group{
		Id: id, Handle: input.Handle, DisplayName: nullableString(input.DisplayName), Enabled: input.Enabled,
		CreatedAt: input.CreatedAt.Time, UpdatedAt: input.UpdatedAt.Time,
	}, nil
}

func tokenResponse(input database.TokenMetadata) (api.Token, error) {
	id, identityID, err := tokenIDs(input)
	if err != nil {
		return api.Token{}, err
	}
	return api.Token{
		Id: id, IdentityId: identityID, Label: input.Label, Status: api.TokenStatus(input.Status),
		ExpiresAt: nullableTimestamp(input.ExpiresAt.Valid, input.ExpiresAt.Time),
		RevokedAt: nullableTimestamp(input.RevokedAt.Valid, input.RevokedAt.Time), CreatedAt: input.CreatedAt.Time,
	}, nil
}

func tokenSecretResponse(input database.CreatedToken) (api.TokenSecret, error) {
	id, identityID, err := tokenIDs(input.Token)
	if err != nil {
		return api.TokenSecret{}, err
	}
	return api.TokenSecret{
		Id: id, IdentityId: identityID, Label: input.Token.Label, Status: api.TokenSecretStatus(input.Token.Status), Secret: input.Secret,
		ExpiresAt: nullableTimestamp(input.Token.ExpiresAt.Valid, input.Token.ExpiresAt.Time),
		RevokedAt: nullableTimestamp(input.Token.RevokedAt.Valid, input.Token.RevokedAt.Time), CreatedAt: input.Token.CreatedAt.Time,
	}, nil
}

func tokenIDs(input database.TokenMetadata) (api.ResourceID, api.ResourceID, error) {
	id, err := uuid.Parse(input.ID)
	if err != nil {
		return api.ResourceID{}, api.ResourceID{}, err
	}
	identityID, err := uuid.Parse(input.IdentityID)
	return id, identityID, err
}

func nullableString(value *string) nullable.Nullable[api.NullableString] {
	if value == nil {
		return nullable.NewNullNullable[api.NullableString]()
	}
	return nullable.NewNullableWithValue(api.NullableString(*value))
}

func nullableTimestamp(valid bool, value time.Time) nullable.Nullable[api.NullableTimestamp] {
	if !valid {
		return nullable.NewNullNullable[api.NullableTimestamp]()
	}
	return nullable.NewNullableWithValue(api.NullableTimestamp(value))
}

func nextCursor(key *page.Key, resource, filters string) (nullable.Nullable[api.NullableString], error) {
	if key == nil {
		return nullable.NewNullNullable[api.NullableString](), nil
	}
	value, err := page.EncodeCursor(resource, filters, *key)
	if err != nil {
		return nil, err
	}
	return nullable.NewNullableWithValue(api.NullableString(value)), nil
}

func setBoolFilter(values url.Values, key string, value *bool) {
	if value != nil {
		values.Set(key, strconv.FormatBool(*value))
	}
}

func setStringFilter[T ~string](values url.Values, key string, value *T) {
	if value != nil {
		values.Set(key, string(*value))
	}
}

func managementError(err error) (api.Error, int) {
	kind := httpapi.KindUnavailable
	switch {
	case errors.Is(err, database.ErrUnauthenticated):
		kind = httpapi.KindUnauthenticated
	case errors.Is(err, database.ErrForbidden):
		kind = httpapi.KindForbidden
	case errors.Is(err, database.ErrNotFound):
		kind = httpapi.KindNotFound
	case errors.Is(err, database.ErrConflict):
		kind = httpapi.KindConflict
	case errors.Is(err, database.ErrInvalid):
		kind = httpapi.KindInvalidRequest
	}
	classified := httpapi.NewError(kind, err)
	body := httpapi.ErrorBody(classified)
	response := api.Error{Error: body.Error}
	if len(body.Errors) != 0 {
		response.Errors = &body.Errors
	}
	return response, httpapi.StatusCode(classified)
}

func listIdentitiesFailure(err error) api.ListIdentitiesdefaultJSONResponse {
	body, status := managementError(err)
	return api.ListIdentitiesdefaultJSONResponse{Body: body, StatusCode: status}
}
func createIdentityFailure(err error) api.CreateIdentitydefaultJSONResponse {
	body, status := managementError(err)
	return api.CreateIdentitydefaultJSONResponse{Body: body, StatusCode: status}
}
func getIdentityFailure(err error) api.GetIdentitydefaultJSONResponse {
	body, status := managementError(err)
	return api.GetIdentitydefaultJSONResponse{Body: body, StatusCode: status}
}
func updateIdentityFailure(err error) api.UpdateIdentitydefaultJSONResponse {
	body, status := managementError(err)
	return api.UpdateIdentitydefaultJSONResponse{Body: body, StatusCode: status}
}
func listGroupsFailure(err error) api.ListGroupsdefaultJSONResponse {
	body, status := managementError(err)
	return api.ListGroupsdefaultJSONResponse{Body: body, StatusCode: status}
}
func createGroupFailure(err error) api.CreateGroupdefaultJSONResponse {
	body, status := managementError(err)
	return api.CreateGroupdefaultJSONResponse{Body: body, StatusCode: status}
}
func getGroupFailure(err error) api.GetGroupdefaultJSONResponse {
	body, status := managementError(err)
	return api.GetGroupdefaultJSONResponse{Body: body, StatusCode: status}
}
func updateGroupFailure(err error) api.UpdateGroupdefaultJSONResponse {
	body, status := managementError(err)
	return api.UpdateGroupdefaultJSONResponse{Body: body, StatusCode: status}
}
func listGroupMembersFailure(err error) api.ListGroupMembersdefaultJSONResponse {
	body, status := managementError(err)
	return api.ListGroupMembersdefaultJSONResponse{Body: body, StatusCode: status}
}
func getCurrentIdentityFailure(err error) api.GetCurrentIdentitydefaultJSONResponse {
	body, status := managementError(err)
	return api.GetCurrentIdentitydefaultJSONResponse{Body: body, StatusCode: status}
}
func listCurrentGroupsFailure(err error) api.ListCurrentIdentityGroupsdefaultJSONResponse {
	body, status := managementError(err)
	return api.ListCurrentIdentityGroupsdefaultJSONResponse{Body: body, StatusCode: status}
}
func listIdentityTokensFailure(err error) api.ListIdentityTokensdefaultJSONResponse {
	body, status := managementError(err)
	return api.ListIdentityTokensdefaultJSONResponse{Body: body, StatusCode: status}
}
func createIdentityTokenFailure(err error) api.CreateIdentityTokendefaultJSONResponse {
	body, status := managementError(err)
	return api.CreateIdentityTokendefaultJSONResponse{Body: body, StatusCode: status}
}
func listCurrentTokensFailure(err error) api.ListCurrentIdentityTokensdefaultJSONResponse {
	body, status := managementError(err)
	return api.ListCurrentIdentityTokensdefaultJSONResponse{Body: body, StatusCode: status}
}
func createCurrentTokenFailure(err error) api.CreateCurrentIdentityTokendefaultJSONResponse {
	body, status := managementError(err)
	return api.CreateCurrentIdentityTokendefaultJSONResponse{Body: body, StatusCode: status}
}

var _ api.StrictServerInterface = (*IAMHandler)(nil)

func (handler *IAMHandler) GetCurrentCredential(ctx context.Context, _ api.GetCurrentCredentialRequestObject) (api.GetCurrentCredentialResponseObject, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		body, status := managementError(err)
		return api.GetCurrentCredentialdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	id, err := uuid.Parse(actor.TokenID)
	if err != nil {
		return nil, err
	}
	return api.GetCurrentCredential200JSONResponse{TokenId: id}, nil
}

func (handler *IAMHandler) ListIdentityGroups(ctx context.Context, request api.ListIdentityGroupsRequestObject) (api.ListIdentityGroupsResponseObject, error) {
	actor, err := requestActor(ctx)
	values := url.Values{"identity_id": {request.IdentityId.String()}}
	setStringFilter(values, "handle_prefix", request.Params.HandlePrefix)
	filters := values.Encode()
	var result database.GroupPage
	if err == nil {
		var after *page.Key
		after, err = decodePageCursor(request.Params.Cursor, "operator-identity-groups", filters)
		if err == nil {
			result, err = handler.store.ListIdentityGroups(ctx, actor, request.IdentityId.String(), database.GroupListOptions{Limit: intValue(request.Params.Limit), After: after, HandlePrefix: request.Params.HandlePrefix})
		}
	}
	if err != nil {
		body, status := managementError(err)
		return api.ListIdentityGroupsdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	response, err := groupPageResponse(result, "operator-identity-groups", filters)
	if err != nil {
		return nil, err
	}
	return api.ListIdentityGroups200JSONResponse(response), nil
}
