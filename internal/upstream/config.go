// Package upstream connects DANS to its single PowerDNS API upstream.
package upstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ncode/dans/internal/httpapi"
)

const (
	MaxKeyBytes      = 4096
	maxFragmentBytes = 64 << 10
)

// TransportConfig selects exactly one restricted PowerDNS transport.
type TransportConfig struct {
	URL        string
	UnixSocket string
	Timeout    time.Duration
}

// KeySources selects exactly one approved plaintext PowerDNS-key source.
type KeySources struct {
	Environment      string
	File             string
	RenderedFragment string
}

// NewHTTPClient constructs a bounded, non-redirecting PowerDNS client.
func NewHTTPClient(config TransportConfig) (*http.Client, *url.URL, error) {
	if config.Timeout <= 0 {
		return nil, nil, errors.New("powerdns transport: timeout must be positive")
	}
	if (config.URL == "") == (config.UnixSocket == "") {
		return nil, nil, errors.New("powerdns transport: configure exactly one URL or Unix socket")
	}

	dialer := &net.Dialer{Timeout: min(config.Timeout, 5*time.Second), KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		DisableCompression:    true,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   20,
		MaxConnsPerHost:       20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   min(config.Timeout, 5*time.Second),
		ResponseHeaderTimeout: config.Timeout,
		ExpectContinueTimeout: time.Second,
	}

	var baseURL *url.URL
	if config.UnixSocket != "" {
		if !filepath.IsAbs(config.UnixSocket) || filepath.Clean(config.UnixSocket) != config.UnixSocket || strings.ContainsRune(config.UnixSocket, 0) {
			return nil, nil, errors.New("powerdns transport: Unix socket path must be clean and absolute")
		}
		baseURL = &url.URL{Scheme: "http", Host: "powerdns"}
		socket := config.UnixSocket
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socket)
		}
	} else {
		parsed, err := url.Parse(config.URL)
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawFragment != "" || parsed.Opaque != "" {
			return nil, nil, errors.New("powerdns transport: invalid URL")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, nil, errors.New("powerdns transport: URL scheme must be http or https")
		}
		if parsed.Path != "" && parsed.Path != "/" {
			return nil, nil, errors.New("powerdns transport: URL must not contain a path")
		}
		parsed.Path = ""
		baseURL = parsed
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   config.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return client, baseURL, nil
}

// LoadKey reads exactly one key source once and returns a redacting wrapper.
func LoadKey(sources KeySources) (httpapi.Secret, error) {
	count := 0
	if sources.Environment != "" {
		count++
	}
	if sources.File != "" {
		count++
	}
	if sources.RenderedFragment != "" {
		count++
	}
	if count != 1 {
		return httpapi.Secret{}, errors.New("powerdns key: configure exactly one source")
	}

	var value string
	var err error
	switch {
	case sources.Environment != "":
		value = sources.Environment
	case sources.File != "":
		value, err = readKeyFile(sources.File)
	case sources.RenderedFragment != "":
		value, err = readRenderedFragment(sources.RenderedFragment)
	}
	if err != nil {
		return httpapi.Secret{}, err
	}
	if err := validateKey(value); err != nil {
		return httpapi.Secret{}, err
	}
	return httpapi.NewSecret(value), nil
}

func readKeyFile(path string) (string, error) {
	data, err := readRegularFile(path, MaxKeyBytes+1)
	if err != nil {
		return "", fmt.Errorf("read powerdns key file: %w", err)
	}
	if len(data) > MaxKeyBytes {
		return "", errors.New("powerdns key file: value is too large")
	}
	data = trimTerminalNewline(data)
	return string(data), nil
}

func readRenderedFragment(path string) (string, error) {
	data, err := readRegularFile(path, maxFragmentBytes+1)
	if err != nil {
		return "", fmt.Errorf("read powerdns rendered fragment: %w", err)
	}
	if len(data) > maxFragmentBytes || strings.ContainsRune(string(data), 0) || strings.ContainsRune(string(data), '\r') {
		return "", errors.New("powerdns rendered fragment: malformed content")
	}
	var value string
	found := 0
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, assignment, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(name) != "api-key" {
			continue
		}
		found++
		value = strings.TrimSpace(assignment)
	}
	if found != 1 {
		return "", errors.New("powerdns rendered fragment: expected exactly one api-key assignment")
	}
	return value, nil
}

func readRegularFile(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("source is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, limit))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	return data, nil
}

func trimTerminalNewline(value []byte) []byte {
	if len(value) >= 2 && value[len(value)-2] == '\r' && value[len(value)-1] == '\n' {
		return value[:len(value)-2]
	}
	if len(value) >= 1 && value[len(value)-1] == '\n' {
		return value[:len(value)-1]
	}
	return value
}

func validateKey(value string) error {
	if value == "" || len(value) > MaxKeyBytes || strings.ContainsAny(value, "\x00\r\n") {
		return errors.New("powerdns key: malformed value")
	}
	return nil
}
