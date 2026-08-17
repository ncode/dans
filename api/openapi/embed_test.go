package openapi

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestEmbeddedContractIsExactAndImmutableToCallers(t *testing.T) {
	t.Parallel()

	first := JSON()
	if !json.Valid(first) {
		t.Fatal("embedded contract is not valid JSON")
	}
	for _, required := range [][]byte{
		[]byte(`"operationId": "listServer"`),
		[]byte(`"operationId": "getLiveness"`),
		[]byte(`"url": "/api/v1"`),
	} {
		if !bytes.Contains(first, required) {
			t.Errorf("embedded contract does not contain %s", required)
		}
	}
	first[0] = 'x'
	if !json.Valid(JSON()) {
		t.Fatal("caller mutated embedded contract")
	}
	if _, err := Spec(); err != nil {
		t.Fatalf("Spec: %v", err)
	}
}
