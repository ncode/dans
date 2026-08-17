package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestBundle_appliesOverlay(t *testing.T) {
	t.Parallel()

	source, overlay := writeFixture(t)
	got, err := bundle(source, overlay)
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}

	document, err := openapi3.NewLoader().LoadFromData(got)
	if err != nil {
		t.Fatalf("load bundled document: %v", err)
	}
	if document.Info.Title != "combined" {
		t.Errorf("title = %q, want %q", document.Info.Title, "combined")
	}
	if operation := document.Paths.Find("/readyz").Get; operation == nil || operation.OperationID != "getReadiness" {
		t.Errorf("GET /readyz = %#v, want getReadiness operation", operation)
	}
}

func TestBundle_isDeterministic(t *testing.T) {
	t.Parallel()

	source, overlay := writeFixture(t)
	first, err := bundle(source, overlay)
	if err != nil {
		t.Fatalf("first bundle: %v", err)
	}
	second, err := bundle(source, overlay)
	if err != nil {
		t.Fatalf("second bundle: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("identical inputs produced different bundle bytes")
	}
}

func writeFixture(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()
	source := filepath.Join(dir, "source.yaml")
	overlay := filepath.Join(dir, "overlay.yaml")
	if err := os.WriteFile(source, []byte(`openapi: 3.1.0
info:
  title: source
  version: "1"
paths: {}
`), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(overlay, []byte(`overlay: 1.0.0
info:
  title: fixture
  version: "1"
actions:
  - target: "$"
    update:
      info:
        title: combined
      paths:
        /readyz:
          get:
            operationId: getReadiness
            responses:
              "200":
                description: ready
`), 0o600); err != nil {
		t.Fatalf("write overlay: %v", err)
	}
	return source, overlay
}
