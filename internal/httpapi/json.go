package httpapi

import (
	"bytes"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
)

var errDuplicateJSONKey = errors.New("duplicate JSON object key")

// StrictJSON bounds and strictly decodes one object before invoking next.
// The callback is never invoked for malformed, duplicate-key, unknown-field,
// or type-invalid input.
func StrictJSON[T any](body io.Reader, maxBytes int64, next func(T) error) error {
	var zero T
	if body == nil || next == nil || maxBytes <= 0 || maxBytes == math.MaxInt64 {
		return NewError(KindInternal, errors.New("invalid strict JSON configuration"))
	}
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return NewError(KindBadRequest, fmt.Errorf("read request body: %w", err))
	}
	if int64(len(data)) > maxBytes {
		return NewError(KindBodyTooLarge, nil)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return NewError(KindBadRequest, io.ErrUnexpectedEOF)
	}
	if err := validateJSONTokens(data); err != nil {
		return NewError(KindBadRequest, err)
	}

	var raw any
	rawDecoder := json.NewDecoder(bytes.NewReader(data))
	rawDecoder.UseNumber()
	if err := rawDecoder.Decode(&raw); err != nil {
		return NewError(KindBadRequest, err)
	}
	if _, ok := raw.(map[string]any); !ok {
		return NewError(KindInvalidRequest, errors.New("request body must be an object"))
	}
	if err := validateJSONShape(raw, reflect.TypeOf(zero)); err != nil {
		return NewError(KindInvalidRequest, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value T
	if err := decoder.Decode(&value); err != nil {
		return NewError(KindInvalidRequest, err)
	}
	return next(value)
}

func validateJSONTokens(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("%w: %s", errDuplicateJSONKey, key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return errors.New("invalid JSON object closing token")
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return errors.New("invalid JSON array closing token")
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	return nil
}

func validateJSONShape(value any, target reflect.Type) error {
	for target != nil && target.Kind() == reflect.Pointer {
		if value == nil {
			return nil
		}
		target = target.Elem()
	}
	if target == nil || value == nil || hasCustomJSONDecoder(target) {
		return nil
	}

	switch target.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return errors.New("JSON value is not an object")
		}
		fields := jsonFields(target)
		for name, fieldValue := range object {
			fieldType, ok := fields[name]
			if !ok {
				return errors.New("unknown JSON object field")
			}
			if err := validateJSONShape(fieldValue, fieldType); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		array, ok := value.([]any)
		if !ok {
			return errors.New("JSON value is not an array")
		}
		for _, item := range array {
			if err := validateJSONShape(item, target.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		object, ok := value.(map[string]any)
		if !ok {
			return errors.New("JSON value is not an object")
		}
		for _, item := range object {
			if err := validateJSONShape(item, target.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

func jsonFields(target reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type)
	for i := range target.NumField() {
		field := target.Field(i)
		if field.PkgPath != "" {
			continue
		}
		tag := field.Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			continue
		}
		if field.Anonymous && name == "" {
			for embeddedName, embeddedType := range jsonFields(indirectType(field.Type)) {
				fields[embeddedName] = embeddedType
			}
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = field.Type
	}
	return fields
}

func indirectType(target reflect.Type) reflect.Type {
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	return target
}

func hasCustomJSONDecoder(target reflect.Type) bool {
	unmarshaler := reflect.TypeFor[json.Unmarshaler]()
	textUnmarshaler := reflect.TypeFor[encoding.TextUnmarshaler]()
	pointer := reflect.PointerTo(target)
	return target.Implements(unmarshaler) || pointer.Implements(unmarshaler) ||
		target.Implements(textUnmarshaler) || pointer.Implements(textUnmarshaler)
}
