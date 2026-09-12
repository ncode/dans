//go:build integration

package database

import (
	"errors"
	"testing"
	"time"

	delegationdomain "github.com/ncode/dans/internal/delegation"
	"github.com/ncode/dans/internal/identifier"
)

func TestBrowserSessionsUseCurrentCredentialsAcrossInstances(t *testing.T) {
	t.Parallel()
	conn, schema, dsn := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	first := NewStore(conn)
	second := NewStore(connectToTestSchema(t, dsn, schema))
	bootstrap, err := first.Bootstrap(t.Context(), BootstrapInput{Handle: "browser-user", TokenLabel: "primary"})
	if err != nil {
		t.Fatal(err)
	}
	one, err := first.CreateBrowserSession(t.Context(), bootstrap.Token.Secret, "")
	if err != nil {
		t.Fatal(err)
	}
	two, err := second.CreateBrowserSession(t.Context(), bootstrap.Token.Secret, "")
	if err != nil {
		t.Fatal(err)
	}
	if one.Secret == two.Secret || !ValidBrowserSession(one.Secret) {
		t.Fatal("sessions must have distinct canonical secrets")
	}
	if remaining := time.Until(one.ExpiresAt); remaining <= 6*24*time.Hour || remaining > 7*24*time.Hour {
		t.Fatalf("session lifetime = %s", remaining)
	}
	digest := identifier.DigestToken(one.Secret)
	var stored []byte
	if err := conn.QueryRow(t.Context(), "SELECT digest FROM browser_sessions WHERE digest = $1", digest[:]).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if string(stored) == one.Secret || string(stored) == bootstrap.Token.Secret {
		t.Fatal("plaintext retained")
	}
	tuple := []RRsetTuple{{Owner: "www.example.org.", RecordType: "A", ChangeKind: "REPLACE"}}
	check := func(secret string, allowed bool) {
		t.Helper()
		actor, authErr := second.Authenticate(t.Context(), secret)
		decision, decisionErr := second.AuthorizeRRsetBatch(t.Context(), secret, "browser-decision", "default", "example.org.", tuple)
		if allowed {
			if authErr != nil || actor.TokenID != bootstrap.Token.Token.ID || decisionErr != nil || !decision.Allowed {
				t.Fatalf("active session: actor=%+v auth=%v decision=%+v err=%v", actor, authErr, decision, decisionErr)
			}
		} else if !errors.Is(authErr, ErrUnauthenticated) || !errors.Is(decisionErr, ErrUnauthenticated) {
			t.Fatalf("unusable session: auth=%v decision=%v", authErr, decisionErr)
		}
	}
	check(one.Secret, true)
	if err := first.DeleteBrowserSession(t.Context(), one.Secret); err != nil {
		t.Fatal(err)
	}
	check(one.Secret, false)
	check(two.Secret, true)
	check(bootstrap.Token.Secret, true)
	for _, invalid := range []string{"invalid", "dans_v1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"} {
		if _, err := first.CreateBrowserSession(t.Context(), invalid, two.Secret); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("invalid login error = %v", err)
		}
		check(two.Secret, true)
	}
	replacement, err := first.CreateBrowserSession(t.Context(), bootstrap.Token.Secret, two.Secret)
	if err != nil {
		t.Fatal(err)
	}
	check(two.Secret, false)
	check(replacement.Secret, true)
	for _, tt := range []struct{ name, sql string }{
		{"session expiry", `UPDATE browser_sessions SET created_at=statement_timestamp()-interval '8 days', expires_at=statement_timestamp()-interval '1 day'`},
		{"disabled identity", `UPDATE identities SET enabled=false`},
		{"token expiry", `UPDATE api_tokens SET created_at=statement_timestamp()-interval '2 days', expires_at=statement_timestamp()-interval '1 day'`},
		{"token revocation", `UPDATE api_tokens SET revoked_at=statement_timestamp()`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := conn.Exec(t.Context(), tt.sql); err != nil {
				t.Fatal(err)
			}
			check(replacement.Secret, false)
			if tt.name != "session expiry" {
				if _, err := first.CreateBrowserSession(t.Context(), bootstrap.Token.Secret, ""); !errors.Is(err, ErrUnauthenticated) {
					t.Fatalf("inactive token login error = %v", err)
				}
			}
			if _, err := conn.Exec(t.Context(), `UPDATE identities SET enabled=true; UPDATE api_tokens SET expires_at=NULL,revoked_at=NULL`); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(t.Context(), `UPDATE browser_sessions SET created_at=statement_timestamp(),expires_at=statement_timestamp()+interval '7 days' WHERE digest=$1`, identifierDigest(replacement.Secret)); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := first.FinalizeRestore(t.Context(), RestoreFinalizationInput{Handle: "browser-user", TokenLabel: "restored"}); err != nil {
		t.Fatal(err)
	}
	check(replacement.Secret, false)
}

func identifierDigest(secret string) []byte {
	digest := identifier.DigestToken(secret)
	return digest[:]
}

func TestBrowserSessionDoesNotCacheDelegatedAuthority(t *testing.T) {
	t.Parallel()
	conn, schema, dsn := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	first, second := NewStore(conn), NewStore(connectToTestSchema(t, dsn, schema))
	operator, _ := insertTestOperatorWithSecret(t, conn, "701")
	identity := insertTestIdentity(t, conn, "702", "session-writer")
	_, token := insertTestActorTokenWithSecret(t, conn, identity, "703")
	binding := insertTestZoneBinding(t, conn, operator.IdentityID, "704", "example.org.")
	tuple := RRsetTuple{Owner: "www.example.org.", RecordType: "A", ChangeKind: "REPLACE"}
	grant := createIdentityGrant(t, first, operator, binding.ID, identity.ID, []DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: tuple.Owner}}, nil, nil)
	session, err := first.CreateBrowserSession(t.Context(), token, "")
	if err != nil {
		t.Fatal(err)
	}
	if decision := authorizeOne(t, second, session.Secret, "session-before-revoke", binding.PowerDNSZoneID, tuple); !decision.Allowed {
		t.Fatal("session grant rejected")
	}
	if _, err := second.Authenticate(t.Context(), session.Secret); err != nil {
		t.Fatal(err)
	}
	if err := first.RevokeDelegation(t.Context(), operator, grant.ID); err != nil {
		t.Fatal(err)
	}
	if decision := authorizeOne(t, second, session.Secret, "session-after-revoke", binding.PowerDNSZoneID, tuple); decision.Allowed {
		t.Fatal("session retained revoked grant after initial authentication")
	}
}

func TestBrowserSessionExpiryFollowsTokenAndCleanupIsBounded(t *testing.T) {
	t.Parallel()
	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	store := NewStore(conn)
	bootstrap, err := store.Bootstrap(t.Context(), BootstrapInput{Handle: "session-expiry", TokenLabel: "primary"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(t.Context(), `UPDATE api_tokens SET expires_at=statement_timestamp()+interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(t.Context(), `INSERT INTO browser_sessions(digest,token_id,created_at,expires_at)
 SELECT decode(lpad(to_hex(n),64,'0'),'hex'),$1,statement_timestamp()-interval '8 days',statement_timestamp()-interval '1 day'
 FROM generate_series(1,150) AS n`, bootstrap.Token.Token.ID); err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateBrowserSession(t.Context(), bootstrap.Token.Secret, "")
	if err != nil {
		t.Fatal(err)
	}
	if remaining := time.Until(session.ExpiresAt); remaining <= 0 || remaining > time.Hour {
		t.Errorf("token-capped session lifetime=%s", remaining)
	}
	var remaining int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM browser_sessions WHERE expires_at<=statement_timestamp()`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 50 {
		t.Errorf("expired sessions left=%d, want50", remaining)
	}
}
