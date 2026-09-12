package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
)

type browseStoreFunc func(context.Context, database.Actor, string, string, database.BrowseOptions, bool) (database.BrowsePage, error)

func (f browseStoreFunc) BrowseRRsets(ctx context.Context, a database.Actor, u, z string, o database.BrowseOptions, refresh bool) (database.BrowsePage, error) {
	return f(ctx, a, u, z, o, refresh)
}

func TestBrowseHandlerPreservesRequiredNullsAndState(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"indexing", "ready", "stale", "refreshing"} {
		t.Run(state, func(t *testing.T) {
			store := browseStoreFunc(func(_ context.Context, actor database.Actor, u, z string, o database.BrowseOptions, refresh bool) (database.BrowsePage, error) {
				if u != "default" || z != "example.test." || refresh || o.Name != "www" {
					t.Errorf("invalid scope/options: %s %s %+v %v", u, z, o, refresh)
				}
				return database.BrowsePage{Items: []json.RawMessage{}, State: state}, nil
			})
			handler, err := NewBrowseHandler(store, "default", &strictFallback{})
			if err != nil {
				t.Fatal(err)
			}
			request := actorRequest(http.MethodGet, "/", nil)
			response, err := handler.BrowseRRsets(request.Context(), api.BrowseRRsetsRequestObject{ServerId: "localhost", ZoneId: "example.test.", Params: api.BrowseRRsetsParams{Name: new("www")}})
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			if err := response.VisitBrowseRRsetsResponse(recorder); err != nil {
				t.Fatal(err)
			}
			want := 200
			if state == "indexing" {
				want = 202
			}
			if recorder.Code != want {
				t.Fatalf("code %d: %s", recorder.Code, recorder.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"error", "last_refreshed_at", "next_cursor"} {
				value, present := body[key]
				if !present || value != nil {
					t.Errorf("%s missing null: %v", key, body)
				}
			}
			if items, ok := body["items"].([]any); !ok || len(items) != 0 {
				t.Fatalf("items not empty array: %v", body)
			}
		})
	}
}
