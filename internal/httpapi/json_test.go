package httpapi

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type createRequest struct {
	Name   string        `json:"name"`
	Nested nestedRequest `json:"nested"`
}

type nestedRequest struct {
	Enabled bool `json:"enabled"`
}

func TestStrictJSONCallsNextWithOneStrictlyDecodedValue(t *testing.T) {
	t.Parallel()

	called := 0
	err := StrictJSON(strings.NewReader(`{"name":"alice","nested":{"enabled":true}}`), 1024, func(request createRequest) error {
		called++
		if request.Name != "alice" || !request.Nested.Enabled {
			t.Errorf("request = %+v", request)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StrictJSON: %v", err)
	}
	if called != 1 {
		t.Errorf("next called %d times, want 1", called)
	}
}

func TestStrictJSONRejectsMalformedAndDuplicateKeysBeforeNext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "empty", body: ""},
		{name: "malformed", body: `{"name":`},
		{name: "trailing value", body: `{"name":"alice","nested":{"enabled":true}} {}`},
		{name: "duplicate top-level key", body: `{"name":"alice","name":"bob","nested":{"enabled":true}}`},
		{name: "duplicate nested key", body: `{"name":"alice","nested":{"enabled":true,"enabled":false}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			called := false
			err := StrictJSON(strings.NewReader(tt.body), 1024, func(createRequest) error {
				called = true
				return nil
			})
			if StatusCode(err) != 400 {
				t.Errorf("StrictJSON() status = %d, want 400 (error %v)", StatusCode(err), err)
			}
			if called {
				t.Error("next called for malformed or duplicate JSON")
			}
		})
	}
}

func TestStrictJSONRejectsUnknownAndWrongTypedFieldsBeforeNext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "unknown top-level", body: `{"name":"alice","nested":{"enabled":true},"extra":1}`},
		{name: "unknown nested", body: `{"name":"alice","nested":{"enabled":true,"extra":1}}`},
		{name: "wrong case is unknown", body: `{"Name":"alice","nested":{"enabled":true}}`},
		{name: "wrong type", body: `{"name":7,"nested":{"enabled":true}}`},
		{name: "null object", body: `null`},
		{name: "array instead of object", body: `[]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			called := false
			err := StrictJSON(strings.NewReader(tt.body), 1024, func(createRequest) error {
				called = true
				return nil
			})
			if StatusCode(err) != 422 {
				t.Errorf("StrictJSON() status = %d, want 422 (error %v)", StatusCode(err), err)
			}
			if called {
				t.Error("authorization/forward callback called for contract-invalid JSON")
			}
		})
	}
}

func TestStrictJSONBoundsTheBodyAndHandlesReadFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		reader     io.Reader
		maxBytes   int64
		wantStatus int
	}{
		{name: "too large", reader: strings.NewReader(`{"name":"alice","nested":{"enabled":true}}`), maxBytes: 8, wantStatus: 413},
		{name: "read failure", reader: failingReader{}, maxBytes: 1024, wantStatus: 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			called := false
			err := StrictJSON(tt.reader, tt.maxBytes, func(createRequest) error {
				called = true
				return nil
			})
			if StatusCode(err) != tt.wantStatus {
				t.Errorf("StrictJSON() status = %d, want %d (error %v)", StatusCode(err), tt.wantStatus, err)
			}
			if called {
				t.Error("next called after body read rejection")
			}
		})
	}
}

func TestStrictJSONPropagatesDownstreamError(t *testing.T) {
	t.Parallel()

	want := errors.New("authorization unavailable")
	err := StrictJSON(strings.NewReader(`{"name":"alice","nested":{"enabled":true}}`), 1024, func(createRequest) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Errorf("StrictJSON() error = %v, want %v", err, want)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
