package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/httpapi"
)

const MaxProbeBytes = 64 << 10

var (
	ErrUpstreamAuthentication = errors.New("powerdns authentication failed")
	ErrUpstreamProbe          = errors.New("powerdns probe failed")
	ErrIncompatibleVersion    = errors.New("incompatible powerdns version")
)

// Client is the generated PowerDNS client plus DANS compatibility checks.
type Client struct {
	generated *api.Client
}

// New constructs a client which replaces any caller-supplied PowerDNS key.
func New(config TransportConfig, key httpapi.Secret) (*Client, error) {
	value := key.Value()
	if err := validateKey(value); err != nil {
		return nil, err
	}

	httpClient, baseURL, err := NewHTTPClient(config)
	if err != nil {
		return nil, err
	}
	generated, err := api.NewClient(
		baseURL.String()+"/api/v1",
		api.WithHTTPClient(httpClient),
	)
	if err != nil {
		return nil, errors.New("construct powerdns client")
	}
	httpClient.Transport = authenticatedTransport{next: httpClient.Transport, key: key}
	return &Client{generated: generated}, nil
}

// Probe authenticates to the authoritative server and enforces the supported version window.
func (client *Client) Probe(ctx context.Context) error {
	response, err := client.generated.ListServer(ctx, api.ServerId("localhost"))
	if err != nil {
		return ErrUpstreamProbe
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUpstreamAuthentication
	case http.StatusOK:
	default:
		return ErrUpstreamProbe
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, MaxProbeBytes+1))
	if err != nil || len(body) > MaxProbeBytes {
		return ErrUpstreamProbe
	}
	var server api.Server
	if err := json.Unmarshal(body, &server); err != nil {
		return ErrUpstreamProbe
	}
	if server.DaemonType == nil || *server.DaemonType != "authoritative" || server.Version == nil || !supportedVersion(*server.Version) {
		return ErrIncompatibleVersion
	}
	return nil
}

func supportedVersion(version string) bool {
	core := version
	if index := strings.IndexAny(core, "-+"); index >= 0 {
		core = core[:index]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	patch, errPatch := strconv.Atoi(parts[2])
	return errMajor == nil && errMinor == nil && errPatch == nil && major == 5 && minor == 1 && patch >= 3
}
