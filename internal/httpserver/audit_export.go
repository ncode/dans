package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
)

const auditExportPath = "/api/v1/dans/audit-events/export"
const auditExportPageTimeout = 30 * time.Second

func isAuditExport(request *http.Request) bool {
	return request.Method == http.MethodGet && request.URL.Path == auditExportPath
}

type reauthenticate func(context.Context) (database.Actor, error)

// auditExportServer owns transport streaming outside the generated strict JSON adapter.
type auditExportServer struct {
	api.ServerInterface
	store AuditStore
}

func (server *auditExportServer) ExportAuditEvents(w http.ResponseWriter, request *http.Request, params api.ExportAuditEventsParams) {
	options, _, err := auditListOptions(api.ListAuditEventsParams{ActorId: params.ActorId, Action: params.Action, TargetType: params.TargetType, TargetId: params.TargetId, Result: params.Result})
	if err != nil {
		writeMutationError(w, RequestIDFromContext(request.Context()), err)
		return
	}
	options.Limit = 500
	authenticate, ok := request.Context().Value(reauthenticateKey).(reauthenticate)
	if !ok || server.store == nil {
		writeMutationError(w, RequestIDFromContext(request.Context()), database.ErrUnauthenticated)
		return
	}
	if err := request.Context().Err(); err != nil {
		writeMutationError(w, RequestIDFromContext(request.Context()), err)
		return
	}
	// Initial admission, authentication, and compatibility keep the ordinary request
	// deadline. Only this authorized handler uses the client-cancelled parent for
	// its independently bounded pages.
	if parent, ok := request.Context().Value(exportParentKey).(context.Context); ok {
		request = request.WithContext(parent)
	}
	controller := http.NewResponseController(w)
	started := false
	fail := func(err error) {
		if started {
			panic(http.ErrAbortHandler)
		}
		writeMutationError(w, RequestIDFromContext(request.Context()), err)
	}
	for {
		if err := request.Context().Err(); err != nil {
			fail(err)
			return
		}
		if err := controller.SetWriteDeadline(time.Now().Add(2 * auditExportPageTimeout)); err != nil {
			fail(err)
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), auditExportPageTimeout)
		actor, err := authenticate(ctx)
		if err == nil && !actor.Operator {
			err = database.ErrForbidden
		}
		var result database.AuditPage
		if err == nil {
			actor.RequestID = RequestIDFromContext(request.Context())
			result, err = server.store.ListAuditEvents(ctx, actor, options)
		}
		cancel()
		if err != nil {
			fail(err)
			return
		}
		if err := request.Context().Err(); err != nil {
			fail(err)
			return
		}
		// Reset only this response's deadline, retaining a bounded stalled write.
		if err := controller.SetWriteDeadline(time.Now().Add(auditExportPageTimeout)); err != nil {
			fail(err)
			return
		}
		if !started {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.Header().Set("Content-Disposition", `attachment; filename="audit-events.ndjson"`)
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			started = true
		}
		encoder := json.NewEncoder(w)
		for _, event := range result.Items {
			if err := encoder.Encode(event); err != nil {
				fail(err)
				return
			}
		}
		if err := controller.Flush(); err != nil {
			fail(err)
			return
		}
		if result.Next == nil {
			return
		}
		if options.After != nil && *options.After == *result.Next {
			fail(errors.New("audit cursor did not advance"))
			return
		}
		options.After = result.Next
	}
}
