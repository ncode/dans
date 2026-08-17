package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// MaxResponseBytes is the largest response body accepted by the DANS client.
const MaxResponseBytes int64 = 64 << 20

const defaultHTTPTimeout = 30 * time.Second

// DANSClientWithResponses is the combined public client. PowerDNS and DANS
// operations use the canonical /api/v1 endpoint, while the document and health
// operations use their ingress-root routes declared by the combined contract.
type DANSClientWithResponses struct {
	*ClientWithResponses
	root ClientInterface
}

// NewDANSClientWithResponses constructs the combined client from a canonical
// endpoint ending in /api/v1.
func NewDANSClientWithResponses(endpoint string, opts ...ClientOption) (*DANSClientWithResponses, error) {
	trimmed := strings.TrimSuffix(endpoint, "/")
	root, ok := strings.CutSuffix(trimmed, "/api/v1")
	if !ok || root == "" {
		return nil, fmt.Errorf("DANS API endpoint must end in /api/v1")
	}
	clientOptions := make([]ClientOption, 0, len(opts)+1)
	clientOptions = append(clientOptions, opts...)
	clientOptions = append(clientOptions, func(client *Client) error {
		if client.Client == nil {
			client.Client = &http.Client{Timeout: defaultHTTPTimeout}
		}
		return nil
	})
	apiClient, err := NewClient(trimmed, clientOptions...)
	if err != nil {
		return nil, err
	}
	apiClient.Client = responseLimitDoer{HttpRequestDoer: apiClient.Client, limit: MaxResponseBytes}
	rootClient := *apiClient
	rootClient.Server = strings.TrimSuffix(root, "/") + "/"
	return &DANSClientWithResponses{
		ClientWithResponses: &ClientWithResponses{ClientInterface: apiClient},
		root:                &rootClient,
	}, nil
}

type responseLimitDoer struct {
	HttpRequestDoer
	limit int64
}

func (doer responseLimitDoer) Do(request *http.Request) (*http.Response, error) {
	response, err := doer.HttpRequestDoer.Do(request)
	if err != nil || response == nil || response.Body == nil {
		return response, err
	}
	response.Body = http.MaxBytesReader(nil, response.Body, doer.limit)
	return response, nil
}

// GetAPIDocument performs the combined contract's ingress-root request.
func (client *DANSClientWithResponses) GetAPIDocument(ctx context.Context, editors ...RequestEditorFn) (*http.Response, error) {
	return client.root.GetAPIDocument(ctx, editors...)
}

// GetAPIDocumentWithResponse performs and parses the ingress-root request.
func (client *DANSClientWithResponses) GetAPIDocumentWithResponse(ctx context.Context, editors ...RequestEditorFn) (*GetAPIDocumentResponse, error) {
	response, err := client.GetAPIDocument(ctx, editors...)
	if err != nil {
		return nil, err
	}
	return ParseGetAPIDocumentResponse(response)
}

// GetLiveness performs the ingress-root liveness request.
func (client *DANSClientWithResponses) GetLiveness(ctx context.Context, editors ...RequestEditorFn) (*http.Response, error) {
	return client.root.GetLiveness(ctx, editors...)
}

// GetLivenessWithResponse performs and parses the ingress-root request.
func (client *DANSClientWithResponses) GetLivenessWithResponse(ctx context.Context, editors ...RequestEditorFn) (*GetLivenessResponse, error) {
	response, err := client.GetLiveness(ctx, editors...)
	if err != nil {
		return nil, err
	}
	return ParseGetLivenessResponse(response)
}

// GetReadiness performs the ingress-root readiness request.
func (client *DANSClientWithResponses) GetReadiness(ctx context.Context, editors ...RequestEditorFn) (*http.Response, error) {
	return client.root.GetReadiness(ctx, editors...)
}

// GetReadinessWithResponse performs and parses the ingress-root request.
func (client *DANSClientWithResponses) GetReadinessWithResponse(ctx context.Context, editors ...RequestEditorFn) (*GetReadinessResponse, error) {
	response, err := client.GetReadiness(ctx, editors...)
	if err != nil {
		return nil, err
	}
	return ParseGetReadinessResponse(response)
}
