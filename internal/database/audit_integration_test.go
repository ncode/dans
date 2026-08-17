//go:build integration

package database

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	delegationdomain "github.com/ncode/dans/internal/delegation"
)

func TestStoreAuthorizationDenialAuditIsSanitizedAndRequired(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "231")
	identity := insertTestIdentity(t, conn, "232", "audited-writer")
	_, secret := insertTestActorTokenWithSecret(t, conn, identity, "332")
	binding := insertTestZoneBinding(t, conn, operator.IdentityID, "410", "audit.test.")
	grant := createIdentityGrant(t, store, operator, binding.ID, identity.ID,
		[]DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "allowed.audit.test."}}, nil, nil)

	decision, err := store.AuthorizeRRsetBatch(t.Context(), secret, "audit-denial-request", "default", "audit.test.", []RRsetTuple{
		{Owner: "denied.audit.test.", RecordType: "TXT", ChangeKind: "DELETE"},
	})
	if err != nil || decision.Allowed {
		t.Fatalf("AuthorizeRRsetBatch(denied) = %+v, %v", decision, err)
	}
	var eventKind, requestID, actorIdentityID, actorTokenID, action, targetKind, targetID, result string
	var details, requestDigest []byte
	if err := conn.QueryRow(t.Context(), `
		SELECT event_kind, request_id, actor_identity_id::text, actor_token_id::text,
		       action, target_kind, target_id, result, details::text, request_digest
		FROM audit_events
		WHERE event_kind = 'authorization_denied' AND request_id = $1`, "audit-denial-request").Scan(
		&eventKind, &requestID, &actorIdentityID, &actorTokenID, &action, &targetKind, &targetID, &result, &details, &requestDigest,
	); err != nil {
		t.Fatalf("read denial audit: %v", err)
	}
	if eventKind != "authorization_denied" || requestID != "audit-denial-request" || actorIdentityID != identity.ID || actorTokenID == "" || action != "powerdns.zone.patch" || targetKind != "powerdns_zone" || targetID != "default:audit.test." || result != "denied" || len(requestDigest) != 32 {
		t.Errorf("denial audit fields = kind %q request %q actor %q/%q action %q target %q/%q result %q digest %d", eventKind, requestID, actorIdentityID, actorTokenID, action, targetKind, targetID, result, len(requestDigest))
	}
	for _, secretValue := range [][]byte{[]byte(secret), []byte(grant.ID), []byte("allowed.audit.test.")} {
		if bytes.Contains(details, secretValue) {
			t.Errorf("denial audit details leaked %q: %s", secretValue, details)
		}
	}

	installRejectAuditTrigger(t, conn)
	failedDecision, err := store.AuthorizeRRsetBatch(t.Context(), secret, "audit-denial-failure", "default", "audit.test.", []RRsetTuple{
		{Owner: "still-denied.audit.test.", RecordType: "A", ChangeKind: "REPLACE"},
	})
	if !errors.Is(err, ErrAuditUnavailable) || failedDecision.Allowed {
		t.Fatalf("AuthorizeRRsetBatch(audit unavailable) = %+v, %v, want denied + %v", failedDecision, err, ErrAuditUnavailable)
	}
}

func TestStoreAuditPaginationFiltersAndNDJSON(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "236")
	identity := insertTestIdentity(t, conn, "237", "audit-reader-subject")
	if _, err := store.CreateGroup(t.Context(), operator, GroupCreate{Handle: "audit-page-one"}); err != nil {
		t.Fatalf("CreateGroup(first) error = %v", err)
	}
	if _, err := store.CreateGroup(t.Context(), operator, GroupCreate{Handle: "audit-page-two"}); err != nil {
		t.Fatalf("CreateGroup(second) error = %v", err)
	}
	if _, err := store.CreateToken(t.Context(), operator, identity.ID, TokenCreate{Label: "audit-token"}); err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}

	action := "group.create"
	first, err := store.ListAuditEvents(t.Context(), operator, AuditListOptions{Limit: 1, Action: &action})
	if err != nil || len(first.Items) != 1 || first.Next == nil {
		t.Fatalf("ListAuditEvents(first) = %+v, %v", first, err)
	}
	second, err := store.ListAuditEvents(t.Context(), operator, AuditListOptions{Limit: 1, Action: &action, After: first.Next})
	if err != nil || len(second.Items) != 1 || second.Items[0].ID == first.Items[0].ID {
		t.Fatalf("ListAuditEvents(second) = %+v, %v", second, err)
	}
	actorID := operator.IdentityID
	filtered, err := store.ListAuditEvents(t.Context(), operator, AuditListOptions{Limit: 100, ActorID: &actorID, Action: &action})
	if err != nil || len(filtered.Items) != 2 {
		t.Fatalf("ListAuditEvents(filtered) = %+v, %v", filtered, err)
	}

	nonOperator := insertTestActorToken(t, conn, identity, "337")
	if _, err := store.ListAuditEvents(t.Context(), nonOperator, AuditListOptions{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("ListAuditEvents(non-operator) error = %v, want %v", err, ErrForbidden)
	}
	var output bytes.Buffer
	if err := store.WriteAuditNDJSON(t.Context(), operator, AuditListOptions{Limit: 1, Action: &action}, &output); err != nil {
		t.Fatalf("WriteAuditNDJSON() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("NDJSON line count = %d, want 2: %s", len(lines), output.String())
	}
	for _, line := range lines {
		var event AuditRecord
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode NDJSON line: %v", err)
		}
		if event.Action != action || event.ID == "" {
			t.Errorf("NDJSON event = %+v", event)
		}
		if strings.Contains(line, "dans_v1_") || strings.Contains(line, "digest") {
			t.Errorf("NDJSON exposed credential material: %s", line)
		}
	}
}

func TestStoreDNSIntentAndExactlyOneOutcome(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	actor := insertTestOperator(t, conn, "234")
	requestDigest := bytes.Repeat([]byte{0x11}, 32)
	intent, err := store.CreateDNSIntent(t.Context(), actor, DNSIntentInput{
		Action: "powerdns.zone.patch", TargetKind: "powerdns_zone", TargetID: "default:intent.test.",
		RRsets:        []RRsetTuple{{Owner: "host.intent.test.", RecordType: "A", ChangeKind: "REPLACE"}},
		RequestDigest: requestDigest, Deadline: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateDNSIntent() error = %v", err)
	}
	if intent.EventID == "" || intent.OperationID == "" {
		t.Errorf("CreateDNSIntent() = %+v", intent)
	}
	var intentResult string
	if err := conn.QueryRow(t.Context(), `SELECT result FROM audit_events WHERE id = $1 AND event_kind = 'dns_intent'`, intent.EventID).Scan(&intentResult); err != nil {
		t.Fatalf("read durable intent before simulated forward: %v", err)
	}
	if intentResult != "pending" {
		t.Errorf("intent result = %q, want pending", intentResult)
	}

	responseDigest := bytes.Repeat([]byte{0x22}, 32)
	responseCode := 204
	outcome, err := store.RecordDNSOutcome(t.Context(), DNSOutcomeInput{
		IntentEventID: intent.EventID, OperationID: intent.OperationID,
		Result: "succeeded", ResponseClass: "success", ResponseCode: &responseCode,
		ResponseDigest: responseDigest,
	})
	if err != nil {
		t.Fatalf("RecordDNSOutcome() error = %v", err)
	}
	if outcome.EventKind != "dns_outcome" || outcome.Result != "succeeded" {
		t.Errorf("RecordDNSOutcome() = %+v", outcome)
	}
	repeated, err := store.RecordDNSOutcome(t.Context(), DNSOutcomeInput{
		IntentEventID: intent.EventID, OperationID: intent.OperationID,
		Result: "succeeded", ResponseClass: "success", ResponseCode: &responseCode,
		ResponseDigest: responseDigest,
	})
	if err != nil || repeated.ID != outcome.ID {
		t.Fatalf("RecordDNSOutcome(repeat) = %+v, %v, want same event", repeated, err)
	}
	if _, err := store.RecordDNSOutcome(t.Context(), DNSOutcomeInput{
		IntentEventID: intent.EventID, OperationID: intent.OperationID, Result: "failed", ResponseClass: "server_error", ResponseCode: &responseCode,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("RecordDNSOutcome(conflict) error = %v, want %v", err, ErrConflict)
	}
	var outcomes int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM audit_events WHERE operation_id = $1 AND event_kind = 'dns_outcome'`, intent.OperationID).Scan(&outcomes); err != nil {
		t.Fatalf("count outcomes: %v", err)
	}
	if outcomes != 1 {
		t.Errorf("outcome rows = %d, want 1", outcomes)
	}
}

func TestStoreDNSIntentFailsClosedWithoutAuditStorage(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	actor := insertTestOperator(t, conn, "235")
	installRejectAuditTrigger(t, conn)

	intent, err := store.CreateDNSIntent(t.Context(), actor, DNSIntentInput{
		Action: "powerdns.zone.patch", TargetKind: "powerdns_zone", TargetID: "default:blocked.test.",
		RRsets:        []RRsetTuple{{Owner: "host.blocked.test.", RecordType: "A", ChangeKind: "REPLACE"}},
		RequestDigest: bytes.Repeat([]byte{0x33}, 32), Deadline: time.Now().Add(time.Minute),
	})
	if !errors.Is(err, ErrAuditUnavailable) || intent.EventID != "" {
		t.Fatalf("CreateDNSIntent(audit unavailable) = %+v, %v, want %v", intent, err, ErrAuditUnavailable)
	}
	var intents int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM audit_events WHERE event_kind = 'dns_intent'`).Scan(&intents); err != nil {
		t.Fatalf("count blocked intents: %v", err)
	}
	if intents != 0 {
		t.Errorf("intent rows after audit failure = %d, want 0", intents)
	}
}

func TestStoreOutcomePersistenceFailurePreservesPendingIntent(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	actor := insertTestOperator(t, conn, "247")
	intent, err := store.CreateDNSIntent(t.Context(), actor, DNSIntentInput{
		Action: "powerdns.zone.patch", TargetKind: "powerdns_zone", TargetID: "default:observed.test.",
		RRsets:        []RRsetTuple{{Owner: "host.observed.test.", RecordType: "A", ChangeKind: "REPLACE"}},
		RequestDigest: bytes.Repeat([]byte{0x34}, 32), Deadline: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateDNSIntent() error = %v", err)
	}
	installRejectAuditTrigger(t, conn)
	code := 204
	if _, err := store.RecordDNSOutcome(t.Context(), DNSOutcomeInput{
		IntentEventID: intent.EventID, OperationID: intent.OperationID,
		Result: "succeeded", ResponseClass: "success", ResponseCode: &code,
		ResponseDigest: bytes.Repeat([]byte{0x35}, 32),
	}); !errors.Is(err, ErrAuditUnavailable) {
		t.Fatalf("RecordDNSOutcome(audit unavailable) error = %v, want %v", err, ErrAuditUnavailable)
	}
	var pending, outcomes int
	if err := conn.QueryRow(t.Context(), `
		SELECT
			(SELECT count(*) FROM audit_events WHERE id = $1 AND result = 'pending'),
			(SELECT count(*) FROM audit_events WHERE intent_event_id = $1 AND event_kind = 'dns_outcome')`, intent.EventID,
	).Scan(&pending, &outcomes); err != nil {
		t.Fatalf("read unmatched observed intent: %v", err)
	}
	if pending != 1 || outcomes != 0 {
		t.Errorf("observed response audit state = pending %d, outcomes %d; want 1, 0", pending, outcomes)
	}
}

func TestStoreProbeAuditPersistenceDetectsFailureWithoutPollutingLedger(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	if err := store.ProbeAuditPersistence(t.Context()); err != nil {
		t.Fatalf("ProbeAuditPersistence() error = %v", err)
	}
	var events int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM audit_events`).Scan(&events); err != nil {
		t.Fatalf("count audit events after successful probe: %v", err)
	}
	if events != 0 {
		t.Errorf("successful audit probe retained %d events, want 0", events)
	}

	installRejectAuditTrigger(t, conn)
	if err := store.ProbeAuditPersistence(t.Context()); !errors.Is(err, ErrAuditUnavailable) {
		t.Fatalf("ProbeAuditPersistence(rejected) error = %v, want %v", err, ErrAuditUnavailable)
	}
	if _, err := conn.Exec(t.Context(), `
		DROP TRIGGER reject_audit_insert ON audit_events;
		DROP FUNCTION reject_audit_insert()`); err != nil {
		t.Fatalf("restore audit persistence: %v", err)
	}
	if err := store.ProbeAuditPersistence(t.Context()); err != nil {
		t.Fatalf("ProbeAuditPersistence(recovered) error = %v", err)
	}
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM audit_events`).Scan(&events); err != nil {
		t.Fatalf("count audit events after recovery probe: %v", err)
	}
	if events != 0 {
		t.Errorf("audit probes retained %d events, want 0", events)
	}
}

func TestStoreCloseOverdueDNSIntentsIsBoundedAndIdempotent(t *testing.T) {
	t.Parallel()

	conn1, schema, dsn := newTestSchema(t)
	if err := Migrate(t.Context(), conn1); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	conn2 := connectToTestSchema(t, dsn, schema)
	for i := 0; i < 4; i++ {
		insertOverdueIntent(t, conn1, i)
	}

	counts := make([]int, 2)
	errs := runConcurrently(
		func() error {
			var err error
			counts[0], err = NewStore(conn1).CloseOverdueDNSIntents(t.Context(), 2)
			return err
		},
		func() error {
			var err error
			counts[1], err = NewStore(conn2).CloseOverdueDNSIntents(t.Context(), 2)
			return err
		},
	)
	if countErrors(errs, nil) != 2 {
		t.Fatalf("concurrent overdue closer errors = %v, want no errors", errs)
	}
	closed := counts[0] + counts[1]
	for {
		count, err := NewStore(conn1).CloseOverdueDNSIntents(t.Context(), 2)
		if err != nil {
			t.Fatalf("CloseOverdueDNSIntents(follow-up) error = %v", err)
		}
		closed += count
		if count == 0 {
			break
		}
	}
	if closed != 4 {
		t.Errorf("total closed intents = %d, want 4", closed)
	}
	var outcomes, pendingWithoutOutcome int
	if err := conn1.QueryRow(t.Context(), `
		SELECT
			(SELECT count(*) FROM audit_events WHERE event_kind = 'dns_outcome' AND result = 'unknown'),
			(SELECT count(*) FROM audit_events AS intent
			 WHERE intent.event_kind = 'dns_intent'
			   AND NOT EXISTS (SELECT 1 FROM audit_events AS outcome WHERE outcome.operation_id = intent.operation_id AND outcome.event_kind = 'dns_outcome'))`).Scan(
		&outcomes, &pendingWithoutOutcome,
	); err != nil {
		t.Fatalf("count overdue outcomes: %v", err)
	}
	if outcomes != 4 || pendingWithoutOutcome != 0 {
		t.Errorf("overdue outcome counts = outcomes %d, pending %d; want 4, 0", outcomes, pendingWithoutOutcome)
	}
}

func TestCloseOverdueDNSIntentsDoesNotPartiallyCloseMismatchedBatch(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	for i := 0; i < 3; i++ {
		insertOverdueIntent(t, conn, i)
	}

	ids, err := New(conn).CloseOverdueDNSIntents(t.Context(), CloseOverdueDNSIntentsParams{
		RowLimit: 3,
		EventIDs: []string{"80000000-0000-4000-8000-000000000401"},
	})
	if err != nil {
		t.Fatalf("CloseOverdueDNSIntents(mismatched batch) error = %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("CloseOverdueDNSIntents(mismatched batch) closed %d intents, want 0", len(ids))
	}

	validEventIDs := []string{
		"80000000-0000-4000-8000-000000000402",
		"80000000-0000-4000-8000-000000000403",
		"80000000-0000-4000-8000-000000000404",
	}
	ids, err = New(conn).CloseOverdueDNSIntents(t.Context(), CloseOverdueDNSIntentsParams{
		RowLimit: 3,
		EventIDs: validEventIDs,
	})
	if err != nil {
		t.Fatalf("CloseOverdueDNSIntents(exact batch) error = %v", err)
	}
	if len(ids) != 3 {
		t.Errorf("CloseOverdueDNSIntents(exact batch) closed %d intents, want 3", len(ids))
	}
}

func insertOverdueIntent(t *testing.T, conn DBTX, index int) {
	t.Helper()
	eventID := fmt.Sprintf("60000000-0000-4000-8000-%012d", 400+index)
	operationID := fmt.Sprintf("70000000-0000-4000-8000-%012d", 400+index)
	if _, err := conn.Exec(t.Context(), `
		INSERT INTO audit_events (
			id, event_kind, operation_id, request_id, action, target_kind,
			target_id, result, deadline_at
		) VALUES ($1, 'dns_intent', $2, $3, 'powerdns.zone.patch',
		          'powerdns_zone', 'default:overdue.test.', 'pending', now() - interval '1 minute')`,
		eventID, operationID, fmt.Sprintf("overdue-request-%d", index),
	); err != nil {
		t.Fatalf("insert overdue intent %d: %v", index, err)
	}
}

func TestStoreManagementMutationRollsBackWhenAuditFails(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "233")
	installRejectAuditTrigger(t, conn)

	if _, err := store.CreateGroup(t.Context(), operator, GroupCreate{Handle: "must-rollback"}); !errors.Is(err, ErrAuditUnavailable) {
		t.Fatalf("CreateGroup(audit unavailable) error = %v, want %v", err, ErrAuditUnavailable)
	}
	var count int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM groups WHERE handle = 'must-rollback'`).Scan(&count); err != nil {
		t.Fatalf("count rolled-back group: %v", err)
	}
	if count != 0 {
		t.Errorf("group rows after rejected audit = %d, want 0", count)
	}
}

func installRejectAuditTrigger(t *testing.T, conn DBTX) {
	t.Helper()
	if _, err := conn.Exec(t.Context(), `
		CREATE FUNCTION reject_audit_insert() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			RAISE EXCEPTION 'audit unavailable' USING ERRCODE = '55000';
		END
		$$;
		CREATE TRIGGER reject_audit_insert
		BEFORE INSERT ON audit_events
		FOR EACH ROW EXECUTE FUNCTION reject_audit_insert()`); err != nil {
		t.Fatalf("install reject audit trigger: %v", err)
	}
}
