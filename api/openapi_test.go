package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oapi-codegen/nullable"
)

func TestEmbeddedContract_isCombined(t *testing.T) {
	t.Parallel()

	document, err := GetSwagger()
	if err != nil {
		t.Fatalf("GetSwagger: %v", err)
	}
	for _, path := range []string{
		"/servers/{server_id}/zones/{zone_id}",
		"/dans/delegations",
		"/livez",
	} {
		if document.Paths.Find(path) == nil {
			t.Errorf("embedded contract has no path %q", path)
		}
	}
}

func TestRequiredNullableResponseFieldsMarshalNull(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		field string
	}{
		{
			name:  "string",
			value: Identity{DisplayName: nullable.NewNullNullable[string]()},
			field: `"display_name":null`,
		},
		{
			name:  "timestamp",
			value: Token{ExpiresAt: nullable.NewNullNullable[time.Time]()},
			field: `"expires_at":null`,
		},
		{
			name:  "resource ID",
			value: AuditEvent{ActorId: nullable.NewNullNullable[ResourceID]()},
			field: `"actor_id":null`,
		},
		{
			name:  "observed zone ID",
			value: ZoneObservation{ObservedZoneId: nullable.NewNullNullable[string]()},
			field: `"observed_zone_id":null`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			if err != nil {
				t.Fatalf("marshal generated response model: %v", err)
			}
			if !strings.Contains(string(encoded), test.field) {
				t.Fatalf("encoded model = %s, want field %s", encoded, test.field)
			}
		})
	}
}

func TestOpaqueAuditTargetResponseFieldMarshals(t *testing.T) {
	t.Parallel()

	event := AuditEvent{TargetId: nullable.NewNullableWithValue[NullableString]("primary:example.org.")}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal generated audit event: %v", err)
	}
	if !strings.Contains(string(encoded), `"target_id":"primary:example.org."`) {
		t.Fatalf("encoded audit event = %s", encoded)
	}
}

func TestAuditTargetFilterUsesBoundedOpaqueString(t *testing.T) {
	t.Parallel()

	target := "primary:example.org."
	params := ListAuditEventsParams{TargetId: &target}
	if params.TargetId == nil || *params.TargetId != target {
		t.Fatalf("generated target filter = %#v", params.TargetId)
	}

	document, err := GetSwagger()
	if err != nil {
		t.Fatalf("GetSwagger: %v", err)
	}
	path := document.Paths.Find("/dans/audit-events")
	if path == nil || path.Get == nil {
		t.Fatal("audit event list operation is missing")
	}
	operation := path.Get
	parameter := operation.Parameters.GetByInAndName("query", "target_id")
	if parameter == nil || parameter.Schema == nil || parameter.Schema.Value == nil {
		t.Fatal("target_id query schema is missing")
	}
	if parameter.Schema.Value.MinLength != 1 || parameter.Schema.Value.MaxLength == nil || *parameter.Schema.Value.MaxLength != 1024 {
		t.Fatalf("target_id query bounds = min %d, max %v", parameter.Schema.Value.MinLength, parameter.Schema.Value.MaxLength)
	}
}

func TestPowerDNSBodyCorrectionsMatchServerHandlers(t *testing.T) {
	t.Parallel()

	document, err := GetSwagger()
	if err != nil {
		t.Fatalf("GetSwagger: %v", err)
	}
	tests := []struct {
		name string
		path string
		ref  string
	}{
		{
			name: "cryptokey update",
			path: "/servers/{server_id}/zones/{zone_id}/cryptokeys/{cryptokey_id}",
			ref:  "#/components/schemas/Cryptokey",
		},
		{
			name: "metadata update",
			path: "/servers/{server_id}/zones/{zone_id}/metadata/{metadata_kind}",
			ref:  "#/components/schemas/Metadata",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := document.Paths.Find(test.path)
			if path == nil || path.Put == nil || path.Put.RequestBody == nil || path.Put.RequestBody.Value == nil {
				t.Fatalf("PUT %s has no request body", test.path)
			}
			if !path.Put.RequestBody.Value.Required {
				t.Fatalf("PUT %s request body is optional", test.path)
			}
			media := path.Put.RequestBody.Value.Content.Get("application/json")
			if media == nil || media.Schema == nil || media.Schema.Ref != test.ref {
				t.Fatalf("PUT %s body schema = %#v, want %q", test.path, media, test.ref)
			}
		})
	}
}
