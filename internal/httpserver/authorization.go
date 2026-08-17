package httpserver

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"

	"github.com/ncode/dans/internal/contract"
	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/httpapi"
)

type AuthorizationDenialAuditor interface {
	RecordAuthorizationDenial(context.Context, database.Actor, database.AuthorizationDenialInput) error
}

// Authorization enforces route-wide classes. A non-operator delegated write
// continues to its handler, which authorizes the complete decoded RRset batch.
func Authorization(auditor AuthorizationDenialAuditor, failures AuditFailureReporter) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			route, ok := RouteInfoFromContext(request.Context())
			if !ok {
				httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindUnavailable, errors.New("request route is not classified")))
				return
			}
			actor, authenticated := ActorFromContext(request.Context())
			if authenticated {
				AccessMetadataFromContext(request.Context()).SetActorID(actor.IdentityID)
			}
			switch route.AccessClass {
			case contract.AccessAnonymous:
				next.ServeHTTP(w, request)
			case contract.AccessAuthenticatedRead, contract.AccessDelegatedWrite:
				if !authenticated {
					httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindUnauthenticated, errors.New("authenticated route without actor")))
					return
				}
				next.ServeHTTP(w, request)
			case contract.AccessOperatorOnly:
				if !authenticated {
					httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindUnauthenticated, errors.New("operator route without actor")))
					return
				}
				if !actor.Operator {
					actor.RequestID = RequestIDFromContext(request.Context())
					digest := sha256.Sum256([]byte(request.Method + "\n" + route.Template))
					if auditor == nil || failures == nil || auditor.RecordAuthorizationDenial(request.Context(), actor, database.AuthorizationDenialInput{
						Action: route.OperationID, TargetKind: "http_route", TargetID: route.Template,
						Details: map[string]any{"access_class": route.AccessClass}, RequestDigest: digest[:],
					}) != nil {
						if failures != nil {
							failures.ReportAuditFailure()
						}
					}
					httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindForbidden, errors.New("operator authority required")))
					return
				}
				next.ServeHTTP(w, request)
			default:
				httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindUnavailable, errors.New("unknown request access class")))
			}
		})
	}
}
