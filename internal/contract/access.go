// Package contract validates invariants in the combined OpenAPI contract.
package contract

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

const accessClassExtension = "x-dans-access-class"

// AccessClass is the authorization boundary attached to an API operation.
type AccessClass string

const (
	AccessAnonymous         AccessClass = "anonymous"
	AccessAuthenticatedRead AccessClass = "authenticated-read"
	AccessDelegatedWrite    AccessClass = "delegated-write"
	AccessOperatorOnly      AccessClass = "operator-only"
)

// ValidateAccessClasses verifies that every operation has one known class and
// that the manifest contains exactly the operation IDs in the document.
func ValidateAccessClasses(document *openapi3.T, classes map[string]AccessClass) error {
	if document == nil || document.Paths == nil {
		return fmt.Errorf("contract has no paths")
	}

	seen := make(map[string]struct{}, len(classes))
	paths := slices.Sorted(maps.Keys(document.Paths.Map()))
	for _, path := range paths {
		operations := document.Paths.Value(path).Operations()
		methods := slices.Sorted(maps.Keys(operations))
		for _, method := range methods {
			operation := operations[method]
			if operation.OperationID == "" {
				return fmt.Errorf("operation %s %s has no operationId", strings.ToUpper(method), path)
			}
			if _, duplicate := seen[operation.OperationID]; duplicate {
				return fmt.Errorf("duplicate operationId %q", operation.OperationID)
			}

			class, ok := classes[operation.OperationID]
			if !ok {
				return fmt.Errorf("missing classification for operation ID %q", operation.OperationID)
			}
			if !class.valid() {
				return fmt.Errorf("operation %q has unknown access class %q", operation.OperationID, class)
			}

			extension, ok := operation.Extensions[accessClassExtension].(string)
			if !ok {
				return fmt.Errorf("operation %q has no string %s", operation.OperationID, accessClassExtension)
			}
			if AccessClass(extension) != class {
				return fmt.Errorf("operation %q access class %q does not match manifest %q", operation.OperationID, extension, class)
			}
			seen[operation.OperationID] = struct{}{}
		}
	}

	for operationID := range classes {
		if _, ok := seen[operationID]; !ok {
			return fmt.Errorf("classification for unknown operation ID %q", operationID)
		}
	}
	return nil
}

func (c AccessClass) valid() bool {
	switch c {
	case AccessAnonymous, AccessAuthenticatedRead, AccessDelegatedWrite, AccessOperatorOnly:
		return true
	default:
		return false
	}
}
