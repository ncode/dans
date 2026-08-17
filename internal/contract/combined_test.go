package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestCombinedAccessClasses(t *testing.T) {
	t.Parallel()

	document := loadCombinedDocument(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi", "access-classes.json"))
	if err != nil {
		t.Fatalf("read access-class manifest: %v", err)
	}
	var classes map[string]AccessClass
	if err := json.Unmarshal(data, &classes); err != nil {
		t.Fatalf("decode access-class manifest: %v", err)
	}
	if err := ValidateAccessClasses(document, classes); err != nil {
		t.Fatal(err)
	}
}

func TestPowerDNSRRSetCompatibility(t *testing.T) {
	t.Parallel()

	document := loadCombinedDocument(t)
	operation := document.Paths.Find("/servers/{server_id}/zones/{zone_id}").Patch
	schema := operation.RequestBody.Value.Content.Get("application/json").Schema.Value

	tests := []struct {
		name string
		body string
	}{
		{
			name: "delete without ttl or records",
			body: `{"rrsets":[{"name":"www.example.com.","type":"A","changetype":"DELETE"}]}`,
		},
		{
			name: "extend without ttl",
			body: `{"rrsets":[{"name":"www.example.com.","type":"A","changetype":"EXTEND","records":[{"content":"192.0.2.1","disabled":false}]}]}`,
		},
		{
			name: "prune without ttl",
			body: `{"rrsets":[{"name":"1.2.0.192.in-addr.arpa.","type":"PTR","changetype":"PRUNE","records":[{"content":"host.example.com."}]}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := schema.VisitJSON(decodeJSON(t, tt.body)); err != nil {
				t.Errorf("request body rejected: %v", err)
			}
		})
	}
}

func TestPowerDNSRRSetCompatibility_rejectsUndeclaredProperty(t *testing.T) {
	t.Parallel()

	document := loadCombinedDocument(t)
	operation := document.Paths.Find("/servers/{server_id}/zones/{zone_id}").Patch
	schema := operation.RequestBody.Value.Content.Get("application/json").Schema.Value
	body := decodeJSON(t, `{"rrsets":[{"name":"www.example.com.","type":"A","changetype":"DELETE","extension":true}]}`)
	if err := schema.VisitJSON(body); err == nil {
		t.Fatal("request body with an undeclared RRset property validated")
	}
}

func loadCombinedDocument(t *testing.T) *openapi3.T {
	t.Helper()

	document, err := openapi3.NewLoader().LoadFromFile(filepath.Join("..", "..", "api", "openapi", "openapi.json"))
	if err != nil {
		t.Fatalf("load combined OpenAPI document: %v", err)
	}
	return document
}

func decodeJSON(t *testing.T, input string) any {
	t.Helper()

	var value any
	if err := json.Unmarshal([]byte(input), &value); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return value
}
