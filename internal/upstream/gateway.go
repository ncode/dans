package upstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/httpapi"
)

// Operation constructs and sends one request through the generated client.
type Operation func(api.ClientInterface) (*http.Response, error)

// Forward executes one typed generated-client operation without retries.
// Genuine upstream HTTP responses, including errors, are returned untouched.
func (client *Client) Forward(operation Operation) (*http.Response, error) {
	if operation == nil {
		return nil, httpapi.NewError(httpapi.KindInternal, errors.New("nil upstream operation"))
	}
	response, err := operation(client.generated)
	if err == nil && response != nil {
		return response, nil
	}
	if err == nil {
		err = errors.New("upstream returned no response")
	}
	if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		return nil, httpapi.NewError(httpapi.KindGatewayTimeout, err)
	}
	return nil, httpapi.NewError(httpapi.KindBadGateway, err)
}

// Relay copies an observed PowerDNS response while removing transport-only and
// credential headers and applying DANS-owned authenticated-response headers.
func Relay(destination http.ResponseWriter, response *http.Response, requestID string) error {
	if response == nil || response.Body == nil {
		return errors.New("relay upstream response: missing response body")
	}
	defer response.Body.Close()

	blocked := connectionHeaderNames(response.Header)
	for name, values := range response.Header {
		if isHopByHop(name) || isBlockedHeader(name, blocked) || strings.EqualFold(name, "X-API-Key") || strings.EqualFold(name, "X-Request-ID") || strings.EqualFold(name, "Cache-Control") {
			continue
		}
		for _, value := range values {
			destination.Header().Add(name, value)
		}
	}
	destination.Header().Set("Cache-Control", "no-store")
	destination.Header().Set("X-Request-ID", requestID)
	destination.WriteHeader(response.StatusCode)
	_, err := io.Copy(destination, response.Body)
	return err
}

type authenticatedTransport struct {
	next http.RoundTripper
	key  httpapi.Secret
}

func (transport authenticatedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = allowedRequestHeaders(request.Header)
	clone.Header.Set("X-API-Key", transport.key.Value())
	clone.Header.Set("User-Agent", "")
	clone.Host = ""
	clone.Close = false
	clone.TransferEncoding = nil
	clone.Trailer = nil
	return transport.next.RoundTrip(clone)
}

func allowedRequestHeaders(source http.Header) http.Header {
	destination := make(http.Header, 3)
	blocked := connectionHeaderNames(source)
	for _, name := range []string{"Accept", "Content-Type"} {
		if isBlockedHeader(name, blocked) {
			continue
		}
		for _, value := range source.Values(name) {
			destination.Add(name, value)
		}
	}
	return destination
}

func connectionHeaderNames(header http.Header) map[string]struct{} {
	blocked := make(map[string]struct{})
	for _, value := range header.Values("Connection") {
		for name := range strings.SplitSeq(value, ",") {
			name = http.CanonicalHeaderKey(strings.TrimSpace(name))
			if name != "" {
				blocked[name] = struct{}{}
			}
		}
	}
	return blocked
}

func isBlockedHeader(name string, blocked map[string]struct{}) bool {
	_, ok := blocked[http.CanonicalHeaderKey(name)]
	return ok
}

func isHopByHop(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Proxy-Connection", "Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	default:
		return false
	}
}

func isTimeout(err error) bool {
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}
