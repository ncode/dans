package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/ncode/dans/api"
)

// MaxBrowseRRsetBytes bounds a single complete RRset, independently of zone size.
const MaxBrowseRRsetBytes = 8 << 20

// StreamZoneRRsets streams complete API RRsets, retaining at most one set. A
// filtered read returning no sets is an authoritative RRset deletion.
func (client *Client) StreamZoneRRsets(ctx context.Context, zoneID, name, recordType string, yield func(json.RawMessage) error) error {
	params := &api.ListZoneParams{IncludeDisabled: new(true)}
	if name != "" {
		params.RrsetName = &name
		params.RrsetType = &recordType
	}
	response, err := client.Forward(func(c api.ClientInterface) (*http.Response, error) {
		return c.ListZone(ctx, "localhost", api.ZoneId(zoneID), params)
	})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return ErrBrowseZoneAbsent
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("read browse zone: upstream status %d", response.StatusCode)
	}
	return streamRRsets(ctx, response.Body, yield)
}

// ErrBrowseZoneAbsent reports an authoritative 404, not a missing RRset.
var ErrBrowseZoneAbsent = errors.New("upstream browse zone absent")

func streamRRsets(ctx context.Context, body io.Reader, yield func(json.RawMessage) error) error {
	// ponytail: 1 GiB per refresh and 8 MiB per RRset; raise measured bounds if
	// supported zones exceed these ceilings, without buffering whole zones.
	total := &io.LimitedReader{R: body, N: 1 << 30}
	value := &io.LimitedReader{R: total, N: MaxBrowseRRsetBytes + 1}
	decoder := json.NewDecoder(value)
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("read browse zone: invalid object")
	}
	seen := false
	for decoder.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		value.N = MaxBrowseRRsetBytes + 1
		token, err = decoder.Token()
		if err != nil {
			return fmt.Errorf("read browse field: %w", err)
		}
		if token != "rrsets" {
			var discarded json.RawMessage
			if err := decoder.Decode(&discarded); err != nil || len(discarded) > MaxBrowseRRsetBytes {
				return errors.New("read browse zone: invalid metadata")
			}
			continue
		}
		if seen {
			return errors.New("read browse zone: duplicate rrsets")
		}
		seen = true
		token, err = decoder.Token()
		if err != nil || token != json.Delim('[') {
			return errors.New("read browse zone: invalid rrsets")
		}
		for count := 0; decoder.More(); count++ {
			if count >= 1000000 {
				return errors.New("read browse zone: too many rrsets")
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			value.N = MaxBrowseRRsetBytes + 1
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				return fmt.Errorf("read browse rrset: %w", err)
			}
			if len(raw) > MaxBrowseRRsetBytes {
				return errors.New("read browse rrset: too large")
			}
			if err := yield(raw); err != nil {
				return err
			}
		}
		token, err = decoder.Token()
		if err != nil || token != json.Delim(']') {
			return errors.New("read browse zone: incomplete rrsets")
		}
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') || !seen {
		return errors.New("read browse zone: incomplete object")
	}
	value.N = MaxBrowseRRsetBytes + 1
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF || total.N <= 0 {
		return errors.New("read browse zone: trailing or oversized response")
	}
	return ctx.Err()
}
