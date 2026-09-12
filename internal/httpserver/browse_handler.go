package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
)

// BrowseStore serves only API-observed DNS data, independently of write grants.
type BrowseStore interface {
	BrowseRRsets(context.Context, database.Actor, string, string, database.BrowseOptions, bool) (database.BrowsePage, error)
}

type BrowseHandler struct {
	api.StrictServerInterface
	store      BrowseStore
	upstreamID string
}

func NewBrowseHandler(store BrowseStore, upstreamID string, next api.StrictServerInterface) (*BrowseHandler, error) {
	if store == nil || upstreamID == "" || next == nil {
		return nil, errors.New("browse HTTP handler: missing dependency")
	}
	return &BrowseHandler{StrictServerInterface: next, store: store, upstreamID: upstreamID}, nil
}

func (h *BrowseHandler) BrowseRRsets(ctx context.Context, r api.BrowseRRsetsRequestObject) (api.BrowseRRsetsResponseObject, error) {
	options := database.BrowseOptions{}
	if r.Params.Name != nil {
		options.Name = *r.Params.Name
	}
	if r.Params.Type != nil {
		options.Type = *r.Params.Type
	}
	if r.Params.Match != nil {
		options.Match = string(*r.Params.Match)
	}
	if r.Params.Cursor != nil {
		options.Cursor = *r.Params.Cursor
	}
	page, err := h.page(ctx, r.ServerId, r.ZoneId, options, false)
	if err != nil {
		body, status := managementError(err)
		return api.BrowseRRsetsdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	if page.State == "indexing" {
		return api.BrowseRRsets202JSONResponse(page), nil
	}
	return api.BrowseRRsets200JSONResponse(page), nil
}

func (h *BrowseHandler) RefreshRRsets(ctx context.Context, r api.RefreshRRsetsRequestObject) (api.RefreshRRsetsResponseObject, error) {
	page, err := h.page(ctx, r.ServerId, r.ZoneId, database.BrowseOptions{}, true)
	if err != nil {
		body, status := managementError(err)
		return api.RefreshRRsetsdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	return api.RefreshRRsets202JSONResponse(page), nil
}

func (h *BrowseHandler) page(ctx context.Context, serverID, zoneID string, options database.BrowseOptions, refresh bool) (api.RRsetBrowsePage, error) {
	actor, err := requestActor(ctx)
	if err != nil {
		return api.RRsetBrowsePage{}, err
	}
	if serverID != "localhost" {
		return api.RRsetBrowsePage{}, database.ErrInvalid
	}
	page, err := h.store.BrowseRRsets(ctx, actor, h.upstreamID, zoneID, options, refresh)
	if err != nil {
		return api.RRsetBrowsePage{}, err
	}
	result := api.RRsetBrowsePage{Items: make([]api.RRSet, len(page.Items)), State: api.RRsetBrowsePageState(page.State), Error: nullableString(page.Error), NextCursor: nullableString(page.NextCursor)}
	if page.LastRefreshedAt == nil {
		result.LastRefreshedAt = nullableTimestamp(false, time.Time{})
	} else {
		result.LastRefreshedAt = nullableTimestamp(true, *page.LastRefreshedAt)
	}
	for i, raw := range page.Items {
		if err := json.Unmarshal(raw, &result.Items[i]); err != nil {
			return api.RRsetBrowsePage{}, err
		}
	}
	return result, nil
}
