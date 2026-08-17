//go:build integration

package database

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	delegationdomain "github.com/ncode/dans/internal/delegation"
)

func TestStoreRecordAuthorizationDenialAppendsSanitizedEvidence(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	actor := insertTestOperator(t, conn, "219")
	digest := sha256.Sum256([]byte("DELETE\n/api/v1/servers/{server_id}/zones/{zone_id}"))
	if err := store.RecordAuthorizationDenial(t.Context(), actor, AuthorizationDenialInput{
		Action: "deleteZone", TargetKind: "http_route",
		TargetID: "/api/v1/servers/{server_id}/zones/{zone_id}",
		Details:  map[string]any{"access_class": "operator-only"}, RequestDigest: digest[:],
	}); err != nil {
		t.Fatalf("RecordAuthorizationDenial() error = %v", err)
	}
	var eventKind, action, targetID, result string
	var storedDigest []byte
	if err := conn.QueryRow(t.Context(), `
		SELECT event_kind, action, target_id, result, request_digest
		FROM audit_events WHERE request_id = $1`, actor.RequestID,
	).Scan(&eventKind, &action, &targetID, &result, &storedDigest); err != nil {
		t.Fatalf("read authorization denial: %v", err)
	}
	if eventKind != "authorization_denied" || action != "deleteZone" || targetID != "/api/v1/servers/{server_id}/zones/{zone_id}" || result != "denied" || string(storedDigest) != string(digest[:]) {
		t.Errorf("authorization denial = kind %q action %q target %q result %q digest %x", eventKind, action, targetID, result, storedDigest)
	}
}

func TestStoreAuthorizeRRsetBatchUsesCurrentCompleteGrants(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	operator, operatorSecret := insertTestOperatorWithSecret(t, conn, "220")
	identity := insertTestIdentity(t, conn, "221", "writer-221")
	writer, writerSecret := insertTestActorTokenWithSecret(t, conn, identity, "321")
	binding := insertTestZoneBinding(t, conn, operator.IdentityID, "404", "example.org.")
	group, err := store.CreateGroup(t.Context(), operator, GroupCreate{Handle: "writers-220"})
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	if _, err := store.AddGroupMember(t.Context(), operator, group.ID, identity.ID); err != nil {
		t.Fatalf("AddGroupMember() error = %v", err)
	}
	recordTypes := []string{"A"}
	changeKinds := []string{"REPLACE"}
	direct, err := store.CreateDelegation(t.Context(), operator, DelegationCreate{
		ZoneBindingID: binding.ID, GranteeIdentityID: &identity.ID,
		Selectors:   []DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "api.example.org."}},
		RecordTypes: &recordTypes, ChangeKinds: &changeKinds,
	})
	if err != nil {
		t.Fatalf("CreateDelegation(direct) error = %v", err)
	}
	groupGrant, err := store.CreateDelegation(t.Context(), operator, DelegationCreate{
		ZoneBindingID: binding.ID, GranteeGroupID: &group.ID,
		Selectors: []DelegationSelectorInput{{Kind: delegationdomain.SelectorGlob, Value: "*.apps.example.org."}},
	})
	if err != nil {
		t.Fatalf("CreateDelegation(group) error = %v", err)
	}

	allowed, err := store.AuthorizeRRsetBatch(t.Context(), writerSecret, "decision-allowed", "default", "example.org.", []RRsetTuple{
		{Owner: "API.EXAMPLE.ORG.", RecordType: "A", ChangeKind: "REPLACE"},
		{Owner: "one.two.apps.example.org.", RecordType: "PTR", ChangeKind: "PRUNE"},
	})
	if err != nil {
		t.Fatalf("AuthorizeRRsetBatch(allowed) error = %v", err)
	}
	if !allowed.Allowed || allowed.Actor.IdentityID != writer.IdentityID || allowed.ZoneBindingID != binding.ID || len(allowed.Denied) != 0 {
		t.Errorf("AuthorizeRRsetBatch(allowed) = %+v", allowed)
	}
	if !sameStringSet(allowed.MatchedDelegationIDs, []string{direct.ID, groupGrant.ID}) {
		t.Errorf("matched delegation IDs = %v, want %v", allowed.MatchedDelegationIDs, []string{direct.ID, groupGrant.ID})
	}

	denied, err := store.AuthorizeRRsetBatch(t.Context(), writerSecret, "decision-denied", "default", "example.org.", []RRsetTuple{
		{Owner: "api.example.org.", RecordType: "A", ChangeKind: "REPLACE"},
		{Owner: "private.example.org.", RecordType: "TXT", ChangeKind: "DELETE"},
	})
	if err != nil {
		t.Fatalf("AuthorizeRRsetBatch(denied) error = %v", err)
	}
	if denied.Allowed || len(denied.Denied) != 1 || denied.Denied[0] != (DeniedRRsetTuple{Index: 1, Owner: "private.example.org.", RecordType: "TXT", ChangeKind: "DELETE"}) {
		t.Errorf("AuthorizeRRsetBatch(denied) = %+v", denied)
	}
	if len(denied.MatchedDelegationIDs) != 0 {
		t.Errorf("denied decision leaked matching delegation IDs: %v", denied.MatchedDelegationIDs)
	}

	if _, err := store.AuthorizeRRsetBatch(t.Context(), "dans_v1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "decision-unknown", "default", "example.org.", []RRsetTuple{{Owner: "api.example.org.", RecordType: "A", ChangeKind: "REPLACE"}}); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("AuthorizeRRsetBatch(unknown token) error = %v, want %v", err, ErrUnauthenticated)
	}

	disabled := false
	if _, err := store.PatchGroup(t.Context(), operator, group.ID, GroupPatch{Enabled: &disabled}); err != nil {
		t.Fatalf("PatchGroup(disable) error = %v", err)
	}
	groupDenied, err := store.AuthorizeRRsetBatch(t.Context(), writerSecret, "decision-disabled-group", "default", "example.org.", []RRsetTuple{{Owner: "one.apps.example.org.", RecordType: "PTR", ChangeKind: "PRUNE"}})
	if err != nil || groupDenied.Allowed {
		t.Fatalf("AuthorizeRRsetBatch(disabled group) = %+v, %v, want denied", groupDenied, err)
	}

	operatorDecision, err := store.AuthorizeRRsetBatch(t.Context(), operatorSecret, "decision-operator", "default", "unbound.example.", []RRsetTuple{{Owner: "anything.example.", RecordType: "TXT", ChangeKind: "DELETE"}})
	if err != nil || !operatorDecision.Allowed || !operatorDecision.Actor.Operator {
		t.Fatalf("AuthorizeRRsetBatch(operator bypass) = %+v, %v", operatorDecision, err)
	}
}

func TestStoreAuthorizationSelectorAndRestrictionSemantics(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "222")
	identity := insertTestIdentity(t, conn, "223", "writer-223")
	_, secret := insertTestActorTokenWithSecret(t, conn, identity, "323")
	binding := insertTestZoneBinding(t, conn, operator.IdentityID, "405", "example.com.")

	createIdentityGrant(t, store, operator, binding.ID, identity.ID,
		[]DelegationSelectorInput{
			{Kind: delegationdomain.SelectorExact, Value: "host.example.com."},
			{Kind: delegationdomain.SelectorGlob, Value: "*.apps.example.com."},
			{Kind: delegationdomain.SelectorGlob, Value: "api.*.anchored.example.com."},
			{Kind: delegationdomain.SelectorGlob, Value: "foo_.example.com."},
			{Kind: delegationdomain.SelectorGlob, Value: "foo%.example.com."},
		}, nil, nil)
	createIdentityGrant(t, store, operator, binding.ID, identity.ID,
		[]DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "all-changes.example.com."}}, nil, nil)

	tests := []struct {
		name  string
		tuple RRsetTuple
		want  bool
	}{
		{name: "exact", tuple: RRsetTuple{Owner: "HOST.EXAMPLE.COM.", RecordType: "A", ChangeKind: "REPLACE"}, want: true},
		{name: "exact does not include descendant", tuple: RRsetTuple{Owner: "child.host.example.com.", RecordType: "A", ChangeKind: "REPLACE"}},
		{name: "glob one label", tuple: RRsetTuple{Owner: "one.apps.example.com.", RecordType: "AAAA", ChangeKind: "DELETE"}, want: true},
		{name: "glob crosses labels", tuple: RRsetTuple{Owner: "one.two.apps.example.com.", RecordType: "PTR", ChangeKind: "EXTEND"}, want: true},
		{name: "anchored glob", tuple: RRsetTuple{Owner: "api.one.anchored.example.com.", RecordType: "TXT", ChangeKind: "PRUNE"}, want: true},
		{name: "glob rejects prefix", tuple: RRsetTuple{Owner: "prefix.api.one.anchored.example.com.", RecordType: "TXT", ChangeKind: "PRUNE"}},
		{name: "zone containment", tuple: RRsetTuple{Owner: "one.apps.example.com.invalid.", RecordType: "A", ChangeKind: "REPLACE"}},
		{name: "SQL underscore literal", tuple: RRsetTuple{Owner: "foo_.example.com.", RecordType: "A", ChangeKind: "REPLACE"}, want: true},
		{name: "SQL underscore does not wildcard", tuple: RRsetTuple{Owner: "foox.example.com.", RecordType: "A", ChangeKind: "REPLACE"}},
		{name: "SQL percent literal", tuple: RRsetTuple{Owner: "foo%.example.com.", RecordType: "A", ChangeKind: "REPLACE"}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := store.AuthorizeRRsetBatch(t.Context(), secret, "semantic-"+strings.ReplaceAll(test.name, " ", "-"), "default", "example.com.", []RRsetTuple{test.tuple})
			if err != nil {
				t.Fatalf("AuthorizeRRsetBatch() error = %v", err)
			}
			if decision.Allowed != test.want {
				t.Errorf("AuthorizeRRsetBatch(%+v).Allowed = %t, want %t", test.tuple, decision.Allowed, test.want)
			}
		})
	}

	for _, changeKind := range []string{"REPLACE", "DELETE", "EXTEND", "PRUNE"} {
		decision, err := store.AuthorizeRRsetBatch(t.Context(), secret, "all-change-"+changeKind, "default", "example.com.", []RRsetTuple{{Owner: "all-changes.example.com.", RecordType: "PTR", ChangeKind: changeKind}})
		if err != nil || !decision.Allowed {
			t.Errorf("unrestricted PTR/%s decision = %+v, %v, want allowed", changeKind, decision, err)
		}
	}

	createIdentityGrant(t, store, operator, binding.ID, identity.ID,
		[]DelegationSelectorInput{{Kind: delegationdomain.SelectorGlob, Value: "*.wild.example.com."}}, nil, nil)
	literalWildcard := RRsetTuple{Owner: "*.wild.example.com.", RecordType: "A", ChangeKind: "REPLACE"}
	if decision := authorizeOne(t, store, secret, "literal-wildcard-glob", "example.com.", literalWildcard); decision.Allowed {
		t.Error("glob authorized a literal wildcard owner")
	}
	createIdentityGrant(t, store, operator, binding.ID, identity.ID,
		[]DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "*.wild.example.com."}}, nil, nil)
	if decision := authorizeOne(t, store, secret, "literal-wildcard-exact", "example.com.", literalWildcard); !decision.Allowed {
		t.Error("exact selector did not authorize literal wildcard owner")
	}
	apex := RRsetTuple{Owner: "example.com.", RecordType: "SOA", ChangeKind: "REPLACE"}
	if decision := authorizeOne(t, store, secret, "apex-before-exact", "example.com.", apex); decision.Allowed {
		t.Error("non-exact grants authorized the zone apex")
	}
	createIdentityGrant(t, store, operator, binding.ID, identity.ID,
		[]DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "example.com."}}, nil, nil)
	if decision := authorizeOne(t, store, secret, "apex-exact", "example.com.", apex); !decision.Allowed {
		t.Error("exact selector did not authorize the zone apex")
	}

	aOnly, deleteOnly := []string{"A"}, []string{"DELETE"}
	txtOnly, replaceOnly := []string{"TXT"}, []string{"REPLACE"}
	createIdentityGrant(t, store, operator, binding.ID, identity.ID,
		[]DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "split.example.com."}}, &aOnly, &deleteOnly)
	createIdentityGrant(t, store, operator, binding.ID, identity.ID,
		[]DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "split.example.com."}}, &txtOnly, &replaceOnly)
	if decision := authorizeOne(t, store, secret, "restrictions-not-composed", "example.com.", RRsetTuple{Owner: "split.example.com.", RecordType: "A", ChangeKind: "REPLACE"}); decision.Allowed {
		t.Error("separate record-type and change-kind matches were composed across grants")
	}
	if decision := authorizeOne(t, store, secret, "complete-restriction", "example.com.", RRsetTuple{Owner: "split.example.com.", RecordType: "TXT", ChangeKind: "REPLACE"}); !decision.Allowed {
		t.Error("one complete restricted grant was not accepted")
	}

	reverse := insertTestZoneBinding(t, conn, operator.IdentityID, "406", "2.0.192.in-addr.arpa.")
	createIdentityGrant(t, store, operator, reverse.ID, identity.ID,
		[]DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "1.2.0.192.in-addr.arpa."}}, nil, nil)
	if decision := authorizeOne(t, store, secret, "ptr-owner", "2.0.192.in-addr.arpa.", RRsetTuple{Owner: "1.2.0.192.in-addr.arpa.", RecordType: "PTR", ChangeKind: "REPLACE"}); !decision.Allowed {
		t.Error("exact reverse-zone PTR owner was not authorized")
	}
}

func TestDeniedRRsetDetailsContainOnlySubmittedTupleData(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "224")
	identity := insertTestIdentity(t, conn, "225", "writer-225")
	_, secret := insertTestActorTokenWithSecret(t, conn, identity, "325")
	binding := insertTestZoneBinding(t, conn, operator.IdentityID, "407", "private.test.")
	grant := createIdentityGrant(t, store, operator, binding.ID, identity.ID,
		[]DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "allowed.private.test."}}, nil, nil)

	decision, err := store.AuthorizeRRsetBatch(t.Context(), secret, "denied-details", "default", "private.test.", []RRsetTuple{
		{Owner: "allowed.private.test.", RecordType: "A", ChangeKind: "REPLACE"},
		{Owner: "DENIED.private.test.", RecordType: "TXT", ChangeKind: "DELETE"},
		{Owner: "other.private.test.", RecordType: "PTR", ChangeKind: "PRUNE"},
	})
	if err != nil {
		t.Fatalf("AuthorizeRRsetBatch() error = %v", err)
	}
	want := []DeniedRRsetTuple{
		{Index: 1, Owner: "DENIED.private.test.", RecordType: "TXT", ChangeKind: "DELETE"},
		{Index: 2, Owner: "other.private.test.", RecordType: "PTR", ChangeKind: "PRUNE"},
	}
	if len(decision.Denied) != len(want) || decision.Denied[0] != want[0] || decision.Denied[1] != want[1] {
		t.Fatalf("Denied = %+v, want %+v", decision.Denied, want)
	}
	encoded, err := json.Marshal(decision.Denied)
	if err != nil {
		t.Fatalf("json.Marshal(Denied) error = %v", err)
	}
	for _, private := range []string{grant.ID, "selector", "grantee", identity.ID} {
		if strings.Contains(string(encoded), private) {
			t.Errorf("denied details leaked %q: %s", private, encoded)
		}
	}
}

func TestStoreAuthorizationUsesCrossConnectionDecisionPointState(t *testing.T) {
	t.Parallel()

	conn1, schema, dsn := newTestSchema(t)
	if err := Migrate(t.Context(), conn1); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	conn2 := connectToTestSchema(t, dsn, schema)
	store1, store2 := NewStore(conn1), NewStore(conn2)
	operator, operatorSecret := insertTestOperatorWithSecret(t, conn1, "226")
	identity := insertTestIdentity(t, conn1, "227", "writer-227")
	_, secret := insertTestActorTokenWithSecret(t, conn1, identity, "327")
	binding := insertTestZoneBinding(t, conn1, operator.IdentityID, "408", "current.test.")
	tuple := RRsetTuple{Owner: "host.current.test.", RecordType: "A", ChangeKind: "REPLACE"}
	selectors := []DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: tuple.Owner}}
	first := createIdentityGrant(t, store1, operator, binding.ID, identity.ID, selectors, nil, nil)
	second := createIdentityGrant(t, store1, operator, binding.ID, identity.ID, selectors, nil, nil)

	initial := authorizeOne(t, store1, secret, "cross-initial", "current.test.", tuple)
	if !initial.Allowed || !sameStringSet(initial.MatchedDelegationIDs, []string{first.ID, second.ID}) {
		t.Fatalf("initial overlapping decision = %+v", initial)
	}
	if err := store2.RevokeDelegation(t.Context(), operator, first.ID); err != nil {
		t.Fatalf("RevokeDelegation(first) error = %v", err)
	}
	afterFirstRevoke := authorizeOne(t, store1, secret, "cross-overlap", "current.test.", tuple)
	if !afterFirstRevoke.Allowed || !sameStringSet(afterFirstRevoke.MatchedDelegationIDs, []string{second.ID}) {
		t.Fatalf("decision after one overlapping revoke = %+v", afterFirstRevoke)
	}

	passedDecision := afterFirstRevoke
	if err := store2.RevokeDelegation(t.Context(), operator, second.ID); err != nil {
		t.Fatalf("RevokeDelegation(second) error = %v", err)
	}
	if !passedDecision.Allowed {
		t.Error("an earlier allowed decision was mutated after revocation")
	}
	afterSecondRevoke := authorizeOne(t, store1, secret, "cross-revoked", "current.test.", tuple)
	if afterSecondRevoke.Allowed {
		t.Fatalf("new decision used a delegation revoked on another connection: %+v", afterSecondRevoke)
	}

	group, err := store1.CreateGroup(t.Context(), operator, GroupCreate{Handle: "cross-group"})
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	if _, err := store1.AddGroupMember(t.Context(), operator, group.ID, identity.ID); err != nil {
		t.Fatalf("AddGroupMember() error = %v", err)
	}
	groupTuple := RRsetTuple{Owner: "group.current.test.", RecordType: "PTR", ChangeKind: "PRUNE"}
	if _, err := store1.CreateDelegation(t.Context(), operator, DelegationCreate{
		ZoneBindingID: binding.ID, GranteeGroupID: &group.ID,
		Selectors: []DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: groupTuple.Owner}},
	}); err != nil {
		t.Fatalf("CreateDelegation(group) error = %v", err)
	}
	if decision := authorizeOne(t, store1, secret, "cross-member-present", "current.test.", groupTuple); !decision.Allowed {
		t.Error("current group membership did not authorize")
	}
	if err := store2.RemoveGroupMember(t.Context(), operator, group.ID, identity.ID); err != nil {
		t.Fatalf("RemoveGroupMember() error = %v", err)
	}
	if decision := authorizeOne(t, store1, secret, "cross-member-removed", "current.test.", groupTuple); decision.Allowed {
		t.Error("removed membership remained authorized on another connection")
	}

	operatorDecision, err := store1.AuthorizeRRsetBatch(t.Context(), operatorSecret, "cross-operator", "default", "current.test.", []RRsetTuple{tuple})
	if err != nil || !operatorDecision.Allowed {
		t.Fatalf("operator bypass decision = %+v, %v", operatorDecision, err)
	}
}

func TestStoreAuthorizationSeesCrossInstanceRoleGroupAndTokenChanges(t *testing.T) {
	t.Parallel()

	conn1, schema, dsn := newTestSchema(t)
	if err := Migrate(t.Context(), conn1); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	conn2 := connectToTestSchema(t, dsn, schema)
	store1, store2 := NewStore(conn1), NewStore(conn2)
	operator, operatorSecret := insertTestOperatorWithSecret(t, conn1, "251")
	secondOperator := insertTestOperator(t, conn1, "252")
	tuple := RRsetTuple{Owner: "host.multi-instance.test.", RecordType: "A", ChangeKind: "REPLACE"}

	operatorDecision, err := store1.AuthorizeRRsetBatch(t.Context(), operatorSecret, "multi-role-before", "default", "unbound.test.", []RRsetTuple{tuple})
	if err != nil || !operatorDecision.Allowed {
		t.Fatalf("operator decision before demotion = %+v, %v", operatorDecision, err)
	}
	demoted := false
	if _, err := store2.PatchIdentity(t.Context(), secondOperator, operator.IdentityID, IdentityPatch{Operator: &demoted}); err != nil {
		t.Fatalf("PatchIdentity(demote on second instance) error = %v", err)
	}
	operatorDecision, err = store1.AuthorizeRRsetBatch(t.Context(), operatorSecret, "multi-role-after", "default", "unbound.test.", []RRsetTuple{tuple})
	if err != nil || operatorDecision.Allowed || operatorDecision.Actor.Operator {
		t.Fatalf("operator decision after cross-instance demotion = %+v, %v", operatorDecision, err)
	}

	identity := insertTestIdentity(t, conn1, "253", "multi-instance-writer")
	writer, writerSecret := insertTestActorTokenWithSecret(t, conn1, identity, "353")
	binding := insertTestZoneBinding(t, conn1, secondOperator.IdentityID, "415", "multi-instance.test.")
	group, err := store1.CreateGroup(t.Context(), secondOperator, GroupCreate{Handle: "multi-instance-group"})
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	if _, err := store1.AddGroupMember(t.Context(), secondOperator, group.ID, identity.ID); err != nil {
		t.Fatalf("AddGroupMember() error = %v", err)
	}
	if _, err := store1.CreateDelegation(t.Context(), secondOperator, DelegationCreate{
		ZoneBindingID: binding.ID, GranteeGroupID: &group.ID,
		Selectors: []DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: tuple.Owner}},
	}); err != nil {
		t.Fatalf("CreateDelegation(group) error = %v", err)
	}
	if decision := authorizeOne(t, store1, writerSecret, "multi-group-before", binding.PowerDNSZoneID, tuple); !decision.Allowed {
		t.Fatal("enabled group did not authorize before cross-instance disable")
	}
	disabled := false
	if _, err := store2.PatchGroup(t.Context(), secondOperator, group.ID, GroupPatch{Enabled: &disabled}); err != nil {
		t.Fatalf("PatchGroup(disable on second instance) error = %v", err)
	}
	if decision := authorizeOne(t, store1, writerSecret, "multi-group-after", binding.PowerDNSZoneID, tuple); decision.Allowed {
		t.Fatal("group disabled on another instance remained authorized")
	}
	if err := store2.RevokeToken(t.Context(), secondOperator, identity.ID, writer.TokenID); err != nil {
		t.Fatalf("RevokeToken(on second instance) error = %v", err)
	}
	if _, err := store1.AuthorizeRRsetBatch(t.Context(), writerSecret, "multi-token-after", binding.Upstream, binding.PowerDNSZoneID, []RRsetTuple{tuple}); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("AuthorizeRRsetBatch(after cross-instance token revoke) error = %v, want %v", err, ErrUnauthenticated)
	}
}

func createIdentityGrant(t *testing.T, store *Store, operator Actor, bindingID, identityID string, selectors []DelegationSelectorInput, recordTypes, changeKinds *[]string) DelegationDetails {
	t.Helper()
	grant, err := store.CreateDelegation(t.Context(), operator, DelegationCreate{
		ZoneBindingID: bindingID, GranteeIdentityID: &identityID, Selectors: selectors,
		RecordTypes: recordTypes, ChangeKinds: changeKinds,
	})
	if err != nil {
		t.Fatalf("CreateDelegation() error = %v", err)
	}
	return grant
}

func authorizeOne(t *testing.T, store *Store, secret, requestID, zoneID string, tuple RRsetTuple) AuthorizationDecision {
	t.Helper()
	decision, err := store.AuthorizeRRsetBatch(t.Context(), secret, requestID, "default", zoneID, []RRsetTuple{tuple})
	if err != nil {
		t.Fatalf("AuthorizeRRsetBatch() error = %v", err)
	}
	return decision
}

func sameStringSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(got))
	for _, value := range got {
		seen[value] = true
	}
	for _, value := range want {
		if !seen[value] {
			return false
		}
	}
	return true
}
