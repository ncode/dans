package httpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/httpapi"
	"github.com/ncode/dans/internal/upstream"
)

// DANSOperations is the non-PowerDNS portion of the generated server. Keeping
// this interface narrow makes the compiler prove that PowerDNSProxy explicitly
// implements every upstream operation rather than inheriting one by accident.
type DANSOperations interface {
	BrowseRRsets(http.ResponseWriter, *http.Request, string, string, api.BrowseRRsetsParams)
	RefreshRRsets(http.ResponseWriter, *http.Request, string, string)
	GetAPIDocument(http.ResponseWriter, *http.Request)
	ListAuditEvents(http.ResponseWriter, *http.Request, api.ListAuditEventsParams)
	ExportAuditEvents(http.ResponseWriter, *http.Request, api.ExportAuditEventsParams)
	ListDelegations(http.ResponseWriter, *http.Request, api.ListDelegationsParams)
	CreateDelegation(http.ResponseWriter, *http.Request)
	RevokeDelegation(http.ResponseWriter, *http.Request, api.DelegationID)
	GetDelegation(http.ResponseWriter, *http.Request, api.DelegationID)
	ListGroups(http.ResponseWriter, *http.Request, api.ListGroupsParams)
	CreateGroup(http.ResponseWriter, *http.Request)
	GetGroup(http.ResponseWriter, *http.Request, api.GroupID)
	UpdateGroup(http.ResponseWriter, *http.Request, api.GroupID)
	ListGroupMembers(http.ResponseWriter, *http.Request, api.GroupID, api.ListGroupMembersParams)
	RemoveGroupMember(http.ResponseWriter, *http.Request, api.GroupID, api.IdentityID)
	AddGroupMember(http.ResponseWriter, *http.Request, api.GroupID, api.IdentityID)
	ListIdentities(http.ResponseWriter, *http.Request, api.ListIdentitiesParams)
	CreateIdentity(http.ResponseWriter, *http.Request)
	GetIdentity(http.ResponseWriter, *http.Request, api.IdentityID)
	UpdateIdentity(http.ResponseWriter, *http.Request, api.IdentityID)
	ListIdentityTokens(http.ResponseWriter, *http.Request, api.IdentityID, api.ListIdentityTokensParams)
	CreateIdentityToken(http.ResponseWriter, *http.Request, api.IdentityID)
	RevokeIdentityToken(http.ResponseWriter, *http.Request, api.IdentityID, api.TokenID)
	GetCurrentIdentity(http.ResponseWriter, *http.Request)
	GetCurrentCredential(http.ResponseWriter, *http.Request)
	ListIdentityGroups(http.ResponseWriter, *http.Request, api.IdentityID, api.ListIdentityGroupsParams)
	ListIdentityDelegations(http.ResponseWriter, *http.Request, api.IdentityID, api.ListIdentityDelegationsParams)
	ListIdentityAssignments(http.ResponseWriter, *http.Request, api.IdentityID, api.ListIdentityAssignmentsParams)
	ListCurrentIdentityDelegations(http.ResponseWriter, *http.Request, api.ListCurrentIdentityDelegationsParams)
	ListCurrentIdentityGroups(http.ResponseWriter, *http.Request, api.ListCurrentIdentityGroupsParams)
	ListCurrentIdentityTokens(http.ResponseWriter, *http.Request, api.ListCurrentIdentityTokensParams)
	CreateCurrentIdentityToken(http.ResponseWriter, *http.Request)
	RevokeCurrentIdentityToken(http.ResponseWriter, *http.Request, api.TokenID)
	ListZoneBindings(http.ResponseWriter, *http.Request, api.ListZoneBindingsParams)
	CreateZoneBinding(http.ResponseWriter, *http.Request)
	GetZoneBinding(http.ResponseWriter, *http.Request, api.ZoneBindingID)
	ConfirmZoneBindingAbsent(http.ResponseWriter, *http.Request, api.ZoneBindingID)
	ObserveZoneBinding(http.ResponseWriter, *http.Request, api.ZoneBindingID)
	RebindZoneBinding(http.ResponseWriter, *http.Request, api.ZoneBindingID)
	RetryZoneBindingDeletion(http.ResponseWriter, *http.Request, api.ZoneBindingID)
	GetLiveness(http.ResponseWriter, *http.Request)
	GetReadiness(http.ResponseWriter, *http.Request)
}

// PowerDNSProxy implements every PowerDNS operation through the generated
// client. The embedded handler supplies only the DANS and root operations.
type PowerDNSProxy struct {
	DANSOperations
	BrowserSessions   *BrowserSessions
	Upstream          *upstream.Client
	Mutations         RRsetMutationStore
	Lifecycle         ZoneLifecycleStore
	AuditFailures     AuditFailureReporter
	LifecycleFailures LifecycleFailureReporter
	UpstreamID        string
	MutationTimeout   time.Duration
}

// RRsetMutationStore is the durable decision and audit boundary for a zone
// patch. The upstream request cannot begin before both authorization and intent
// persistence succeed.
type RRsetMutationStore interface {
	AuthorizeRRsetBatch(context.Context, string, string, string, string, []database.RRsetTuple) (database.AuthorizationDecision, error)
	CreateDNSIntent(context.Context, database.Actor, database.DNSIntentInput) (database.DNSIntent, error)
	RecordDNSOutcome(context.Context, database.DNSOutcomeInput) (database.DNSOutcome, error)
}

// ZoneLifecycleStore owns the generation changes which must bracket upstream
// zone creation and deletion.
type ZoneLifecycleStore interface {
	BindCreatedZone(context.Context, database.Actor, database.ZoneBindingInput) (database.ZoneBinding, error)
	PrepareZoneDeletionByUpstreamZoneID(context.Context, database.Actor, database.ZoneDeletionByUpstreamInput) (database.ZoneDeletionPlan, error)
	RecordZoneDeletionOutcome(context.Context, database.ZoneDeletionPlan, database.ZoneDeletionOutcomeInput) (database.DNSOutcome, error)
}

// AuditFailureReporter makes an instance unready after a terminal audit write
// fails, without changing an already-observed upstream response.
type AuditFailureReporter interface {
	ReportAuditFailure()
}

// LifecycleFailureReporter latches an upstream-success/local-binding failure
// until an operator reconciles it and restarts the affected instance.
type LifecycleFailureReporter interface {
	ReportLifecycleFailure()
}

func (proxy *PowerDNSProxy) forward(w http.ResponseWriter, request *http.Request, operation upstream.Operation) {
	metadata := AccessMetadataFromContext(request.Context())
	if proxy == nil || proxy.Upstream == nil {
		metadata.SetUpstreamOutcome("not_configured")
		httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindInternal, errors.New("PowerDNS client is not configured")))
		return
	}
	response, err := proxy.Upstream.Forward(operation)
	if err != nil {
		metadata.SetUpstreamOutcome("transport_error")
		httpapi.WriteError(w, RequestIDFromContext(request.Context()), err)
		return
	}
	metadata.SetUpstreamOutcome("response")
	if err := upstream.Relay(w, response, RequestIDFromContext(request.Context())); err != nil {
		metadata.SetUpstreamOutcome("relay_error")
	}
}

func (proxy *PowerDNSProxy) forwardMutation(
	w http.ResponseWriter,
	request *http.Request,
	action string,
	targetKind string,
	targetID string,
	operation func(api.ClientInterface, io.Reader) (*http.Response, error),
) {
	requestID := RequestIDFromContext(request.Context())
	if proxy == nil || proxy.Upstream == nil || proxy.Mutations == nil || proxy.UpstreamID == "" || proxy.MutationTimeout <= 0 || operation == nil {
		httpapi.WriteError(w, requestID, httpapi.NewError(httpapi.KindUnavailable, errors.New("DNS mutation dependency is not configured")))
		return
	}
	actor, ok := ActorFromContext(request.Context())
	if !ok {
		httpapi.WriteError(w, requestID, httpapi.NewError(httpapi.KindUnauthenticated, database.ErrUnauthenticated))
		return
	}
	actor.RequestID = requestID
	body, err := readBody(request)
	if err != nil {
		httpapi.WriteError(w, requestID, httpapi.NewError(httpapi.KindBadRequest, err))
		return
	}
	digest := sha256.Sum256(body)
	intent, err := proxy.Mutations.CreateDNSIntent(request.Context(), actor, database.DNSIntentInput{
		Action: action, TargetKind: targetKind, TargetID: targetID,
		RequestDigest: digest[:], Deadline: mutationDeadline(request.Context(), proxy.MutationTimeout),
	})
	if err != nil {
		writeMutationError(w, requestID, err)
		return
	}
	proxy.forwardIntent(w, request, intent, func(client api.ClientInterface) (*http.Response, error) {
		return operation(client, bytes.NewReader(body))
	})
}

func (proxy *PowerDNSProxy) resourceTarget(request *http.Request) string {
	return proxy.UpstreamID + ":" + request.URL.EscapedPath()
}

func readBody(request *http.Request) ([]byte, error) {
	if request.Body == nil {
		return nil, nil
	}
	return io.ReadAll(request.Body)
}

func mutationDeadline(ctx context.Context, timeout time.Duration) time.Time {
	deadline := time.Now().Add(timeout)
	if requestDeadline, ok := ctx.Deadline(); ok && requestDeadline.Before(deadline) {
		return requestDeadline
	}
	return deadline
}

func (proxy *PowerDNSProxy) PowerDNSError(w http.ResponseWriter, request *http.Request) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.PowerDNSError(request.Context())
	})
}

func (proxy *PowerDNSProxy) ListServers(w http.ResponseWriter, request *http.Request) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.ListServers(request.Context())
	})
}

func (proxy *PowerDNSProxy) ListServer(w http.ResponseWriter, request *http.Request, serverID api.ServerId) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.ListServer(request.Context(), serverID)
	})
}

func (proxy *PowerDNSProxy) GetAutoprimaries(w http.ResponseWriter, request *http.Request, serverID api.ServerId) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.GetAutoprimaries(request.Context(), serverID)
	})
}

func (proxy *PowerDNSProxy) CreateAutoprimary(w http.ResponseWriter, request *http.Request, serverID api.ServerId) {
	proxy.forwardMutation(w, request, "powerdns.autoprimary.create", "powerdns_resource", proxy.resourceTarget(request), func(client api.ClientInterface, body io.Reader) (*http.Response, error) {
		return client.CreateAutoprimaryWithBody(request.Context(), serverID, request.Header.Get("Content-Type"), body)
	})
}

func (proxy *PowerDNSProxy) DeleteAutoprimary(w http.ResponseWriter, request *http.Request, serverID api.ServerId, ip, nameserver string) {
	proxy.forwardMutation(w, request, "powerdns.autoprimary.delete", "powerdns_resource", proxy.resourceTarget(request), func(client api.ClientInterface, _ io.Reader) (*http.Response, error) {
		return client.DeleteAutoprimary(request.Context(), serverID, ip, nameserver)
	})
}

func (proxy *PowerDNSProxy) CacheFlushByName(w http.ResponseWriter, request *http.Request, serverID api.ServerId, params api.CacheFlushByNameParams) {
	proxy.forwardMutation(w, request, "powerdns.cache.flush", "powerdns_resource", proxy.resourceTarget(request), func(client api.ClientInterface, _ io.Reader) (*http.Response, error) {
		return client.CacheFlushByName(request.Context(), serverID, &params)
	})
}

func (proxy *PowerDNSProxy) GetConfig(w http.ResponseWriter, request *http.Request, serverID api.ServerId) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.GetConfig(request.Context(), serverID)
	})
}

func (proxy *PowerDNSProxy) GetConfigSetting(w http.ResponseWriter, request *http.Request, serverID api.ServerId, name string) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.GetConfigSetting(request.Context(), serverID, name)
	})
}

func (proxy *PowerDNSProxy) ListNetworks(w http.ResponseWriter, request *http.Request, serverID api.ServerId) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.ListNetworks(request.Context(), serverID)
	})
}

func (proxy *PowerDNSProxy) GetNetwork(w http.ResponseWriter, request *http.Request, serverID api.ServerId, ip, prefixLength string) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.GetNetwork(request.Context(), serverID, ip, prefixLength)
	})
}

func (proxy *PowerDNSProxy) SetNetwork(w http.ResponseWriter, request *http.Request, serverID api.ServerId, ip, prefixLength string) {
	proxy.forwardMutation(w, request, "powerdns.network.update", "powerdns_resource", proxy.resourceTarget(request), func(client api.ClientInterface, body io.Reader) (*http.Response, error) {
		return client.SetNetworkWithBody(request.Context(), serverID, ip, prefixLength, request.Header.Get("Content-Type"), body)
	})
}

func (proxy *PowerDNSProxy) SearchData(w http.ResponseWriter, request *http.Request, serverID api.ServerId, params api.SearchDataParams) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.SearchData(request.Context(), serverID, &params)
	})
}

func (proxy *PowerDNSProxy) GetStats(w http.ResponseWriter, request *http.Request, serverID api.ServerId, params api.GetStatsParams) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.GetStats(request.Context(), serverID, &params)
	})
}

func (proxy *PowerDNSProxy) ListTSIGKeys(w http.ResponseWriter, request *http.Request, serverID api.ServerId) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.ListTSIGKeys(request.Context(), serverID)
	})
}

func (proxy *PowerDNSProxy) CreateTSIGKey(w http.ResponseWriter, request *http.Request, serverID api.ServerId) {
	proxy.forwardMutation(w, request, "powerdns.tsig_key.create", "powerdns_resource", proxy.resourceTarget(request), func(client api.ClientInterface, body io.Reader) (*http.Response, error) {
		return client.CreateTSIGKeyWithBody(request.Context(), serverID, request.Header.Get("Content-Type"), body)
	})
}

func (proxy *PowerDNSProxy) DeleteTSIGKey(w http.ResponseWriter, request *http.Request, serverID api.ServerId, keyID string) {
	proxy.forwardMutation(w, request, "powerdns.tsig_key.delete", "powerdns_resource", proxy.resourceTarget(request), func(client api.ClientInterface, _ io.Reader) (*http.Response, error) {
		return client.DeleteTSIGKey(request.Context(), serverID, keyID)
	})
}

func (proxy *PowerDNSProxy) GetTSIGKey(w http.ResponseWriter, request *http.Request, serverID api.ServerId, keyID string) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.GetTSIGKey(request.Context(), serverID, keyID)
	})
}

func (proxy *PowerDNSProxy) PutTSIGKey(w http.ResponseWriter, request *http.Request, serverID api.ServerId, keyID string) {
	proxy.forwardMutation(w, request, "powerdns.tsig_key.update", "powerdns_resource", proxy.resourceTarget(request), func(client api.ClientInterface, body io.Reader) (*http.Response, error) {
		return client.PutTSIGKeyWithBody(request.Context(), serverID, keyID, request.Header.Get("Content-Type"), body)
	})
}

func (proxy *PowerDNSProxy) ListViews(w http.ResponseWriter, request *http.Request, serverID api.ServerId) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.ListViews(request.Context(), serverID)
	})
}

func (proxy *PowerDNSProxy) ListView(w http.ResponseWriter, request *http.Request, serverID api.ServerId, view api.View) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.ListView(request.Context(), serverID, view)
	})
}

func (proxy *PowerDNSProxy) AddToView(w http.ResponseWriter, request *http.Request, serverID api.ServerId, view api.View) {
	proxy.forwardMutation(w, request, "powerdns.view.add", "powerdns_resource", proxy.resourceTarget(request), func(client api.ClientInterface, body io.Reader) (*http.Response, error) {
		return client.AddToViewWithBody(request.Context(), serverID, view, request.Header.Get("Content-Type"), body)
	})
}

func (proxy *PowerDNSProxy) DeleteFromView(w http.ResponseWriter, request *http.Request, serverID api.ServerId, view api.View, id string) {
	proxy.forwardMutation(w, request, "powerdns.view.delete", "powerdns_resource", proxy.resourceTarget(request), func(client api.ClientInterface, _ io.Reader) (*http.Response, error) {
		return client.DeleteFromView(request.Context(), serverID, view, id)
	})
}

func (proxy *PowerDNSProxy) ListZones(w http.ResponseWriter, request *http.Request, serverID api.ServerId, params api.ListZonesParams) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.ListZones(request.Context(), serverID, &params)
	})
}

func (proxy *PowerDNSProxy) CreateZone(w http.ResponseWriter, request *http.Request, serverID api.ServerId, params api.CreateZoneParams) {
	requestID := RequestIDFromContext(request.Context())
	if proxy == nil || proxy.Upstream == nil || proxy.Mutations == nil || proxy.Lifecycle == nil || proxy.AuditFailures == nil || proxy.LifecycleFailures == nil || proxy.UpstreamID == "" || proxy.MutationTimeout <= 0 {
		httpapi.WriteError(w, requestID, httpapi.NewError(httpapi.KindUnavailable, errors.New("zone lifecycle dependency is not configured")))
		return
	}
	actor, ok := ActorFromContext(request.Context())
	if !ok {
		httpapi.WriteError(w, requestID, httpapi.NewError(httpapi.KindUnauthenticated, database.ErrUnauthenticated))
		return
	}
	actor.RequestID = requestID
	body, err := readBody(request)
	if err != nil {
		httpapi.WriteError(w, requestID, httpapi.NewError(httpapi.KindBadRequest, err))
		return
	}
	var requested api.Zone
	if err := json.Unmarshal(body, &requested); err != nil {
		httpapi.WriteError(w, requestID, httpapi.NewError(httpapi.KindBadRequest, err))
		return
	}
	if requested.Name == nil || *requested.Name == "" {
		httpapi.WriteError(w, requestID, httpapi.NewError(httpapi.KindInvalidRequest, database.ErrInvalid))
		return
	}
	digest := sha256.Sum256(body)
	intent, err := proxy.Mutations.CreateDNSIntent(request.Context(), actor, database.DNSIntentInput{
		Action: "powerdns.zone.create", TargetKind: "powerdns_zone", TargetID: proxy.UpstreamID + ":" + *requested.Name,
		RequestDigest: digest[:], Deadline: mutationDeadline(request.Context(), proxy.MutationTimeout),
	})
	if err != nil {
		writeMutationError(w, requestID, err)
		return
	}
	metadata := AccessMetadataFromContext(request.Context())
	metadata.SetOperationID(intent.OperationID)
	response, err := proxy.Upstream.Forward(func(client api.ClientInterface) (*http.Response, error) {
		return client.CreateZoneWithBody(request.Context(), serverID, &params, request.Header.Get("Content-Type"), bytes.NewReader(body))
	})
	if err != nil {
		metadata.SetUpstreamOutcome("transport_error")
		proxy.recordOutcome(request.Context(), intent, database.DNSOutcomeInput{Result: "unknown", ResponseClass: "transport_error"})
		httpapi.WriteError(w, requestID, err)
		return
	}
	responseBody, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(responseBody))
	metadata.SetUpstreamOutcome("response")
	result, responseClass := classifyMutationResponse(response.StatusCode)
	if readErr != nil {
		result, responseClass = "unknown", "response_read_error"
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 && readErr != nil {
		proxy.LifecycleFailures.ReportLifecycleFailure()
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 && readErr == nil {
		var created api.Zone
		if err := json.Unmarshal(responseBody, &created); err != nil || created.Id == nil || created.Name == nil {
			metadata.SetUpstreamOutcome("response_binding_error")
			result, responseClass = "unknown", "binding_error"
			proxy.LifecycleFailures.ReportLifecycleFailure()
		} else {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), proxy.MutationTimeout)
			_, bindErr := proxy.Lifecycle.BindCreatedZone(ctx, actor, database.ZoneBindingInput{
				Upstream: proxy.UpstreamID, PowerDNSZoneID: *created.Id, ZoneName: *created.Name,
			})
			cancel()
			if bindErr != nil {
				metadata.SetUpstreamOutcome("response_binding_error")
				result, responseClass = "unknown", "binding_error"
				proxy.LifecycleFailures.ReportLifecycleFailure()
			}
		}
	}
	responseDigest := sha256.Sum256(responseBody)
	proxy.recordOutcome(request.Context(), intent, database.DNSOutcomeInput{
		Result: result, ResponseClass: responseClass, ResponseCode: &response.StatusCode, ResponseDigest: responseDigest[:],
	})
	if err := upstream.Relay(w, response, requestID); err != nil {
		metadata.SetUpstreamOutcome("relay_error")
	}
}

func (proxy *PowerDNSProxy) DeleteZone(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId) {
	requestID := RequestIDFromContext(request.Context())
	if proxy == nil || proxy.Upstream == nil || proxy.Lifecycle == nil || proxy.UpstreamID == "" || proxy.MutationTimeout <= 0 {
		httpapi.WriteError(w, requestID, httpapi.NewError(httpapi.KindUnavailable, errors.New("zone lifecycle dependency is not configured")))
		return
	}
	actor, ok := ActorFromContext(request.Context())
	if !ok {
		httpapi.WriteError(w, requestID, httpapi.NewError(httpapi.KindUnauthenticated, database.ErrUnauthenticated))
		return
	}
	actor.RequestID = requestID
	digest := sha256.Sum256(nil)
	plan, err := proxy.Lifecycle.PrepareZoneDeletionByUpstreamZoneID(request.Context(), actor, database.ZoneDeletionByUpstreamInput{
		Upstream: proxy.UpstreamID, PowerDNSZoneID: string(zoneID), RequestDigest: digest[:],
		Deadline: mutationDeadline(request.Context(), proxy.MutationTimeout),
	})
	if err != nil {
		writeMutationError(w, requestID, err)
		return
	}
	deletion := zoneDeletion{
		upstream: proxy.Upstream, recordOutcome: proxy.Lifecycle.RecordZoneDeletionOutcome,
		timeout: proxy.MutationTimeout, auditFailures: proxy.AuditFailures,
	}
	response, _, err := deletion.execute(request.Context(), serverID, plan)
	if err != nil {
		httpapi.WriteError(w, requestID, err)
		return
	}
	if err := upstream.Relay(w, response, requestID); err != nil {
		AccessMetadataFromContext(request.Context()).SetUpstreamOutcome("relay_error")
	}
}

func (proxy *PowerDNSProxy) ListZone(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId, params api.ListZoneParams) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.ListZone(request.Context(), serverID, zoneID, &params)
	})
}

func (proxy *PowerDNSProxy) PatchZone(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId) {
	requestID := RequestIDFromContext(request.Context())
	if proxy == nil || proxy.Upstream == nil || proxy.Mutations == nil || proxy.UpstreamID == "" || proxy.MutationTimeout <= 0 {
		httpapi.WriteError(w, requestID, httpapi.NewError(httpapi.KindUnavailable, errors.New("DNS mutation dependency is not configured")))
		return
	}
	body, err := readBody(request)
	if err != nil {
		httpapi.WriteError(w, requestID, httpapi.NewError(httpapi.KindBadRequest, err))
		return
	}
	var patch api.ZonePatch
	if err := json.Unmarshal(body, &patch); err != nil {
		httpapi.WriteError(w, requestID, httpapi.NewError(httpapi.KindBadRequest, err))
		return
	}
	if len(patch.Rrsets) == 0 || len(patch.Rrsets) > database.MaxRRsetBatch {
		httpapi.WriteError(w, requestID, httpapi.NewError(httpapi.KindInvalidRequest, database.ErrInvalid))
		return
	}
	tuples := make([]database.RRsetTuple, len(patch.Rrsets))
	for index, rrset := range patch.Rrsets {
		tuples[index] = database.RRsetTuple{
			Owner: rrset.Name, RecordType: rrset.Type, ChangeKind: string(rrset.Changetype),
		}
	}
	credential, err := proxy.BrowserSessions.requestCredential(request)
	if err != nil {
		httpapi.WriteError(w, requestID, httpapi.NewError(httpapi.KindUnauthenticated, database.ErrUnauthenticated))
		return
	}
	decision, err := proxy.Mutations.AuthorizeRRsetBatch(request.Context(), credential, requestID, proxy.UpstreamID, string(zoneID), tuples)
	if err != nil {
		if errors.Is(err, database.ErrAuditUnavailable) && proxy.AuditFailures != nil {
			proxy.AuditFailures.ReportAuditFailure()
		}
		writeMutationError(w, requestID, err)
		return
	}
	if !decision.Allowed {
		details := make([]string, len(decision.Denied))
		for index, denied := range decision.Denied {
			details[index] = fmt.Sprintf("rrsets[%d]: owner=%q type=%q change=%q", denied.Index, denied.Owner, denied.RecordType, denied.ChangeKind)
		}
		httpapi.WriteError(w, requestID, httpapi.NewDetailedError(httpapi.KindForbidden, database.ErrForbidden, details...))
		return
	}

	digest := sha256.Sum256(body)
	targetKind, targetID := "powerdns_zone", proxy.UpstreamID+":"+string(zoneID)
	if decision.ZoneBindingID != "" {
		targetKind, targetID = "zone_binding", decision.ZoneBindingID
	}
	intent, err := proxy.Mutations.CreateDNSIntent(request.Context(), decision.Actor, database.DNSIntentInput{
		Action: "powerdns.zone.patch", TargetKind: targetKind, TargetID: targetID,
		RRsets: tuples, MatchedDelegationIDs: decision.MatchedDelegationIDs,
		RequestDigest: digest[:], Deadline: mutationDeadline(request.Context(), proxy.MutationTimeout),
	})
	if err != nil {
		writeMutationError(w, requestID, err)
		return
	}

	proxy.forwardIntent(w, request, intent, func(client api.ClientInterface) (*http.Response, error) {
		return client.PatchZoneWithBody(request.Context(), serverID, zoneID, request.Header.Get("Content-Type"), bytes.NewReader(body))
	})
}

func (proxy *PowerDNSProxy) forwardIntent(w http.ResponseWriter, request *http.Request, intent database.DNSIntent, operation upstream.Operation) {
	requestID := RequestIDFromContext(request.Context())
	metadata := AccessMetadataFromContext(request.Context())
	metadata.SetOperationID(intent.OperationID)
	response, err := proxy.Upstream.Forward(operation)
	if err != nil {
		metadata.SetUpstreamOutcome("transport_error")
		proxy.recordOutcome(request.Context(), intent, database.DNSOutcomeInput{
			Result: "unknown", ResponseClass: "transport_error",
		})
		httpapi.WriteError(w, requestID, err)
		return
	}

	metadata.SetUpstreamOutcome("response")
	result, responseClass := classifyMutationResponse(response.StatusCode)
	proxy.recordOutcome(request.Context(), intent, database.DNSOutcomeInput{
		Result: result, ResponseClass: responseClass, ResponseCode: &response.StatusCode,
	})
	if err := upstream.Relay(w, response, requestID); err != nil {
		metadata.SetUpstreamOutcome("relay_error")
	}
}

func (proxy *PowerDNSProxy) recordOutcome(parent context.Context, intent database.DNSIntent, input database.DNSOutcomeInput) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), proxy.MutationTimeout)
	defer cancel()
	input.IntentEventID = intent.EventID
	input.OperationID = intent.OperationID
	if _, err := proxy.Mutations.RecordDNSOutcome(ctx, input); err != nil && proxy.AuditFailures != nil {
		proxy.AuditFailures.ReportAuditFailure()
	}
}

func classifyMutationResponse(status int) (string, string) {
	switch {
	case status >= 200 && status < 300:
		return "succeeded", "success"
	case status >= 400 && status < 500:
		return "failed", "client_error"
	case status >= 500:
		return "failed", "server_error"
	default:
		return "failed", "other"
	}
}

func writeMutationError(w http.ResponseWriter, requestID string, err error) {
	kind := httpapi.KindUnavailable
	switch {
	case errors.Is(err, database.ErrUnauthenticated):
		kind = httpapi.KindUnauthenticated
	case errors.Is(err, database.ErrForbidden):
		kind = httpapi.KindForbidden
	case errors.Is(err, database.ErrInvalid):
		kind = httpapi.KindInvalidRequest
	case errors.Is(err, database.ErrNotFound):
		kind = httpapi.KindNotFound
	case errors.Is(err, database.ErrConflict):
		kind = httpapi.KindConflict
	}
	httpapi.WriteError(w, requestID, httpapi.NewError(kind, err))
}

func (proxy *PowerDNSProxy) PutZone(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId) {
	proxy.forwardMutation(w, request, "powerdns.zone.update", "powerdns_zone", proxy.UpstreamID+":"+string(zoneID), func(client api.ClientInterface, body io.Reader) (*http.Response, error) {
		return client.PutZoneWithBody(request.Context(), serverID, zoneID, request.Header.Get("Content-Type"), body)
	})
}

func (proxy *PowerDNSProxy) AxfrRetrieveZone(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId) {
	proxy.forwardMutation(w, request, "powerdns.zone.axfr_retrieve", "powerdns_zone", proxy.UpstreamID+":"+string(zoneID), func(client api.ClientInterface, _ io.Reader) (*http.Response, error) {
		return client.AxfrRetrieveZone(request.Context(), serverID, zoneID)
	})
}

func (proxy *PowerDNSProxy) ListCryptokeys(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.ListCryptokeys(request.Context(), serverID, zoneID)
	})
}

func (proxy *PowerDNSProxy) CreateCryptokey(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId) {
	proxy.forwardMutation(w, request, "powerdns.cryptokey.create", "powerdns_zone", proxy.UpstreamID+":"+string(zoneID), func(client api.ClientInterface, body io.Reader) (*http.Response, error) {
		return client.CreateCryptokeyWithBody(request.Context(), serverID, zoneID, request.Header.Get("Content-Type"), body)
	})
}

func (proxy *PowerDNSProxy) DeleteCryptokey(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId, keyID string) {
	proxy.forwardMutation(w, request, "powerdns.cryptokey.delete", "powerdns_zone", proxy.UpstreamID+":"+string(zoneID), func(client api.ClientInterface, _ io.Reader) (*http.Response, error) {
		return client.DeleteCryptokey(request.Context(), serverID, zoneID, keyID)
	})
}

func (proxy *PowerDNSProxy) GetCryptokey(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId, keyID string) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.GetCryptokey(request.Context(), serverID, zoneID, keyID)
	})
}

func (proxy *PowerDNSProxy) ModifyCryptokey(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId, keyID string) {
	proxy.forwardMutation(w, request, "powerdns.cryptokey.update", "powerdns_zone", proxy.UpstreamID+":"+string(zoneID), func(client api.ClientInterface, body io.Reader) (*http.Response, error) {
		return client.ModifyCryptokeyWithBody(request.Context(), serverID, zoneID, keyID, request.Header.Get("Content-Type"), body)
	})
}

func (proxy *PowerDNSProxy) AxfrExportZone(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.AxfrExportZone(request.Context(), serverID, zoneID)
	})
}

func (proxy *PowerDNSProxy) ListMetadata(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.ListMetadata(request.Context(), serverID, zoneID)
	})
}

func (proxy *PowerDNSProxy) CreateMetadata(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId) {
	proxy.forwardMutation(w, request, "powerdns.metadata.create", "powerdns_zone", proxy.UpstreamID+":"+string(zoneID), func(client api.ClientInterface, body io.Reader) (*http.Response, error) {
		return client.CreateMetadataWithBody(request.Context(), serverID, zoneID, request.Header.Get("Content-Type"), body)
	})
}

func (proxy *PowerDNSProxy) DeleteMetadata(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId, kind string) {
	proxy.forwardMutation(w, request, "powerdns.metadata.delete", "powerdns_zone", proxy.UpstreamID+":"+string(zoneID), func(client api.ClientInterface, _ io.Reader) (*http.Response, error) {
		return client.DeleteMetadata(request.Context(), serverID, zoneID, kind)
	})
}

func (proxy *PowerDNSProxy) GetMetadata(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId, kind string) {
	proxy.forward(w, request, func(client api.ClientInterface) (*http.Response, error) {
		return client.GetMetadata(request.Context(), serverID, zoneID, kind)
	})
}

func (proxy *PowerDNSProxy) ModifyMetadata(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId, kind string) {
	proxy.forwardMutation(w, request, "powerdns.metadata.update", "powerdns_zone", proxy.UpstreamID+":"+string(zoneID), func(client api.ClientInterface, body io.Reader) (*http.Response, error) {
		return client.ModifyMetadataWithBody(request.Context(), serverID, zoneID, kind, request.Header.Get("Content-Type"), body)
	})
}

func (proxy *PowerDNSProxy) NotifyZone(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId) {
	proxy.forwardMutation(w, request, "powerdns.zone.notify", "powerdns_zone", proxy.UpstreamID+":"+string(zoneID), func(client api.ClientInterface, _ io.Reader) (*http.Response, error) {
		return client.NotifyZone(request.Context(), serverID, zoneID)
	})
}

func (proxy *PowerDNSProxy) RectifyZone(w http.ResponseWriter, request *http.Request, serverID api.ServerId, zoneID api.ZoneId) {
	proxy.forwardMutation(w, request, "powerdns.zone.rectify", "powerdns_zone", proxy.UpstreamID+":"+string(zoneID), func(client api.ClientInterface, _ io.Reader) (*http.Response, error) {
		return client.RectifyZone(request.Context(), serverID, zoneID)
	})
}

var _ api.ServerInterface = (*PowerDNSProxy)(nil)
