package api

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDANSClientRoutesCombinedContractAcrossAPIAndIngressRoots(t *testing.T) {
	t.Parallel()

	doer := &recordingDoer{}
	client, err := NewDANSClientWithResponses("https://dans.example.test/api/v1", WithHTTPClient(doer))
	if err != nil {
		t.Fatalf("NewDANSClientWithResponses() error = %v", err)
	}
	if _, err := client.GetCurrentIdentity(t.Context()); err != nil {
		t.Fatalf("GetCurrentIdentity() error = %v", err)
	}
	if _, err := client.GetAPIDocumentWithResponse(t.Context()); err != nil {
		t.Fatalf("GetAPIDocumentWithResponse() error = %v", err)
	}
	if _, err := client.GetLivenessWithResponse(t.Context()); err != nil {
		t.Fatalf("GetLivenessWithResponse() error = %v", err)
	}
	if _, err := client.GetReadinessWithResponse(t.Context()); err != nil {
		t.Fatalf("GetReadinessWithResponse() error = %v", err)
	}
	if got := doer.paths(); len(got) != 4 || got[0] != "/api/v1/dans/me" || got[1] != "/api/docs" || got[2] != "/livez" || got[3] != "/readyz" {
		t.Fatalf("request paths = %q", got)
	}
}

func TestDANSClientRequiresCanonicalAPIEndpoint(t *testing.T) {
	t.Parallel()

	if _, err := NewDANSClientWithResponses("https://dans.example.test"); err == nil {
		t.Fatal("NewDANSClientWithResponses() accepted endpoint without /api/v1")
	}
}

func TestResponseLimitDoerRejectsOversizedParsedResponses(t *testing.T) {
	body := &trackedReadCloser{Reader: strings.NewReader("four")}
	client, err := NewClientWithResponses(
		"https://dans.example.test/api/v1",
		WithHTTPClient(responseLimitDoer{
			limit: 3,
			HttpRequestDoer: doerFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       body,
					Request:    request,
				}, nil
			}),
		}),
	)
	if err != nil {
		t.Fatalf("NewClientWithResponses() error = %v", err)
	}

	_, err = client.GetCurrentIdentityWithResponse(t.Context())
	tooLarge, ok := errors.AsType[*http.MaxBytesError](err)
	if !ok {
		t.Fatalf("GetCurrentIdentityWithResponse() error = %v, want *http.MaxBytesError", err)
	}
	if tooLarge.Limit != 3 {
		t.Errorf("response limit = %d, want 3", tooLarge.Limit)
	}
	if !body.closed {
		t.Error("oversized response body was not closed")
	}
}

func TestDANSClientUsesBoundedDefaultHTTPClient(t *testing.T) {
	t.Parallel()

	client, err := NewDANSClientWithResponses("https://dans.example.test/api/v1")
	if err != nil {
		t.Fatalf("NewDANSClientWithResponses() error = %v", err)
	}
	generated, ok := client.ClientWithResponses.ClientInterface.(*Client)
	if !ok {
		t.Fatalf("generated client = %T, want *Client", client.ClientWithResponses.ClientInterface)
	}
	limited, ok := generated.Client.(responseLimitDoer)
	if !ok {
		t.Fatalf("HTTP doer = %T, want responseLimitDoer", generated.Client)
	}
	if limited.limit != MaxResponseBytes {
		t.Errorf("response limit = %d, want %d", limited.limit, MaxResponseBytes)
	}
	root, ok := client.root.(*Client)
	if !ok {
		t.Fatalf("root client = %T, want *Client", client.root)
	}
	rootLimited, ok := root.Client.(responseLimitDoer)
	if !ok || rootLimited.limit != MaxResponseBytes {
		t.Fatalf("root HTTP doer = %#v, want responseLimitDoer with limit %d", root.Client, MaxResponseBytes)
	}
	httpClient, ok := limited.HttpRequestDoer.(*http.Client)
	if !ok {
		t.Fatalf("underlying HTTP doer = %T, want *http.Client", limited.HttpRequestDoer)
	}
	if httpClient.Timeout != 30*time.Second {
		t.Errorf("HTTP timeout = %s, want 30s", httpClient.Timeout)
	}
}

func TestDANSClientNilHTTPClientUsesBoundedDefault(t *testing.T) {
	t.Parallel()

	client, err := NewDANSClientWithResponses(
		"https://dans.example.test/api/v1",
		WithHTTPClient(nil),
	)
	if err != nil {
		t.Fatalf("NewDANSClientWithResponses() error = %v", err)
	}
	generated, ok := client.ClientWithResponses.ClientInterface.(*Client)
	if !ok {
		t.Fatalf("generated client = %T, want *Client", client.ClientWithResponses.ClientInterface)
	}
	limited, ok := generated.Client.(responseLimitDoer)
	if !ok {
		t.Fatalf("HTTP doer = %T, want responseLimitDoer", generated.Client)
	}
	if limited.limit != MaxResponseBytes {
		t.Errorf("response limit = %d, want %d", limited.limit, MaxResponseBytes)
	}
	httpClient, ok := limited.HttpRequestDoer.(*http.Client)
	if !ok {
		t.Fatalf("underlying HTTP doer = %T, want *http.Client", limited.HttpRequestDoer)
	}
	if httpClient.Timeout != 30*time.Second {
		t.Errorf("HTTP timeout = %s, want 30s", httpClient.Timeout)
	}
}

type recordingDoer struct {
	mu       sync.Mutex
	requests []*http.Request
}

func (doer *recordingDoer) Do(request *http.Request) (*http.Response, error) {
	doer.mu.Lock()
	doer.requests = append(doer.requests, request)
	doer.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Request:    request,
	}, nil
}

func (doer *recordingDoer) paths() []string {
	doer.mu.Lock()
	defer doer.mu.Unlock()
	paths := make([]string, len(doer.requests))
	for index, request := range doer.requests {
		paths[index] = request.URL.Path
	}
	return paths
}

type doerFunc func(*http.Request) (*http.Response, error)

func (doer doerFunc) Do(request *http.Request) (*http.Response, error) {
	return doer(request)
}

type trackedReadCloser struct {
	io.Reader
	closed bool
}

func (body *trackedReadCloser) Close() error {
	body.closed = true
	return nil
}

var _ ClientWithResponsesInterface = (*DANSClientWithResponses)(nil)
var _ ClientInterface = (*DANSClientWithResponses)(nil)
