package contract

import (
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestValidateAccessClasses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		document string
		classes  map[string]AccessClass
		wantErr  string
	}{
		{
			name:     "complete",
			document: documentWithOperation("getThing", string(AccessAuthenticatedRead)),
			classes:  map[string]AccessClass{"getThing": AccessAuthenticatedRead},
		},
		{
			name:     "missing classification",
			document: documentWithOperation("getThing", string(AccessAuthenticatedRead)),
			classes:  map[string]AccessClass{},
			wantErr:  "missing classification for operation ID",
		},
		{
			name:     "extra operation ID",
			document: documentWithOperation("getThing", string(AccessAuthenticatedRead)),
			classes: map[string]AccessClass{
				"getThing": AccessAuthenticatedRead,
				"ghost":    AccessOperatorOnly,
			},
			wantErr: "classification for unknown operation ID",
		},
		{
			name:     "missing operation ID",
			document: documentWithOperation("", string(AccessAuthenticatedRead)),
			classes:  map[string]AccessClass{},
			wantErr:  "operation GET /things has no operationId",
		},
		{
			name:     "unknown access class",
			document: documentWithOperation("getThing", "superuser"),
			classes:  map[string]AccessClass{"getThing": "superuser"},
			wantErr:  "unknown access class",
		},
		{
			name:     "extension differs from manifest",
			document: documentWithOperation("getThing", string(AccessOperatorOnly)),
			classes:  map[string]AccessClass{"getThing": AccessAuthenticatedRead},
			wantErr:  "does not match manifest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			document, err := openapi3.NewLoader().LoadFromData([]byte(tt.document))
			if err != nil {
				t.Fatalf("load test document: %v", err)
			}

			err = ValidateAccessClasses(document, tt.classes)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateAccessClasses() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateAccessClasses() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func documentWithOperation(operationID, class string) string {
	return `{
	  "openapi": "3.1.0",
	  "info": {"title": "test", "version": "1"},
	  "paths": {
	    "/things": {
	      "get": {
	        "operationId": "` + operationID + `",
	        "x-dans-access-class": "` + class + `",
	        "responses": {"204": {"description": "ok"}}
	      }
	    }
	  }
	}`
}
