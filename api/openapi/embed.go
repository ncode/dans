// Package openapi embeds the exact checked-in combined API contract.
package openapi

import (
	_ "embed"

	"github.com/getkin/kin-openapi/openapi3"
)

//go:embed openapi.json
var document []byte

// JSON returns an independent copy of the exact combined contract bytes.
func JSON() []byte { return append([]byte(nil), document...) }

// Spec parses an independent contract model for request routing and validation.
func Spec() (*openapi3.T, error) {
	return openapi3.NewLoader().LoadFromData(document)
}
