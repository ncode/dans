package database

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ncode/dans/internal/dnsname"
	"github.com/ncode/dans/internal/identifier"
)

type BrowseOptions struct{ Name, Match, Type, Cursor string }

// BrowsePage carries bounded complete RRsets and explicit snapshot freshness.
type BrowsePage struct {
	Items           []json.RawMessage `json:"items"`
	NextCursor      *string           `json:"next_cursor"`
	State           string            `json:"state"`
	LastRefreshedAt *time.Time        `json:"last_refreshed_at"`
	Error           *string           `json:"error"`
}

type browseSnapshot struct {
	ID, Generation string
	Revision       int64
	CursorKey      []byte
}

type browseCursor struct {
	Zone       string `json:"z"`
	Generation string `json:"g"`
	Revision   int64  `json:"v"`
	Name       string `json:"n"`
	Match      string `json:"m"`
	Type       string `json:"t"`
	AfterName  string `json:"a"`
	AfterType  string `json:"b"`
	Expires    int64  `json:"e"`
}

func normalizeBrowseOptions(o BrowseOptions) (BrowseOptions, error) {
	if o.Match == "" {
		o.Match = "prefix"
	}
	if o.Match != "prefix" && o.Match != "exact" || len(o.Name) > 255 || len(o.Cursor) > 2048 {
		return o, ErrInvalid
	}
	o.Name = strings.ToLower(o.Name)
	if o.Name != "" && o.Match == "exact" {
		name, err := dnsname.Parse(strings.TrimSuffix(o.Name, ".") + ".")
		if err != nil {
			return o, ErrInvalid
		}
		o.Name = name.String()
	}
	for _, c := range o.Name {
		if c < 33 || c > 126 {
			return o, ErrInvalid
		}
	}
	if o.Type != "" && !validUniqueRecordTypes([]string{o.Type}) {
		return o, ErrInvalid
	}
	return o, nil
}

func encodeBrowseCursor(s browseSnapshot, o BrowseOptions, name, typ string, expires time.Time) (string, error) {
	payload, err := json.Marshal(browseCursor{s.ID, s.Generation, s.Revision, o.Name, o.Match, o.Type, name, typ, expires.Unix()})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.CursorKey)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac.Sum(nil)...)), nil
}

func decodeBrowseCursor(encoded string, s browseSnapshot, o BrowseOptions) (string, string, error) {
	if len(encoded) > 2048 {
		return "", "", ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) <= sha256.Size {
		return "", "", ErrInvalid
	}
	payload, signature := raw[:len(raw)-sha256.Size], raw[len(raw)-sha256.Size:]
	mac := hmac.New(sha256.New, s.CursorKey)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return "", "", ErrInvalid
	}
	var c browseCursor
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return "", "", ErrInvalid
	}
	if decoder.Decode(new(any)) != io.EOF {
		return "", "", ErrInvalid
	}
	if c.Zone != s.ID || c.Generation != s.Generation || c.Revision != s.Revision || c.Expires <= time.Now().Unix() {
		return "", "", ErrConflict
	}
	if c.Name != o.Name || c.Match != o.Match || c.Type != o.Type || c.AfterName == "" || c.AfterType == "" {
		return "", "", ErrInvalid
	}
	return c.AfterName, c.AfterType, nil
}

// BrowseRRsets authenticates through the caller's current actor and reads a
// single consistent page while holding only the zone metadata row lock.
func (s *Store) BrowseRRsets(ctx context.Context, actor Actor, upstream, zoneID string, options BrowseOptions, refresh bool) (BrowsePage, error) {
	if err := requireActor(actor); err != nil {
		return BrowsePage{}, err
	}
	if !validHandle(upstream) || zoneID == "" || len(zoneID) > 255 || strings.ContainsAny(zoneID, "\x00\r\n") {
		return BrowsePage{}, ErrInvalid
	}
	options, err := normalizeBrowseOptions(options)
	if err != nil {
		return BrowsePage{}, err
	}
	id, err := identifier.NewUUID()
	if err != nil {
		return BrowsePage{}, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return BrowsePage{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return BrowsePage{}, err
	}
	defer rollback(tx)
	q := New(tx)
	zone, err := q.TouchBrowseZone(ctx, TouchBrowseZoneParams{ID: id, Upstream: upstream, ZoneID: zoneID, CursorKey: key})
	if err != nil {
		return BrowsePage{}, fmt.Errorf("touch browse zone: %w", err)
	}
	if refresh {
		if err := q.RequestBrowseRefresh(ctx, zone.ID); err != nil {
			return BrowsePage{}, err
		}
		zone.FullRequested = true
	}
	result := BrowsePage{Items: []json.RawMessage{}, State: "indexing", Error: zone.Error}
	if zone.RefreshedAt.Valid {
		result.LastRefreshedAt = &zone.RefreshedAt.Time
	}
	if zone.Generation != nil {
		pending, err := q.BrowseHasPending(ctx, zone.ID)
		if err != nil {
			return BrowsePage{}, err
		}
		result.State = "ready"
		if pending || zone.Error != nil || zone.RefreshedAt.Time.Before(time.Now().Add(-5*time.Minute)) {
			result.State = "stale"
		}
		if zone.Error == nil && (zone.FullRequested || zone.FullLease != nil) {
			result.State = "refreshing"
		}
		state := browseSnapshot{zone.ID, *zone.Generation, zone.Revision, zone.CursorKey}
		afterName, afterType := "", ""
		if options.Cursor != "" {
			afterName, afterType, err = decodeBrowseCursor(options.Cursor, state, options)
			if err != nil {
				return BrowsePage{}, err
			}
		}

		upper := options.Name + "\x7f"
		if options.Match == "exact" && options.Name != "" {
			upper = options.Name
		}
		var items []ListBrowseRRsetsRow
		if options.Type == "" {
			items, err = q.ListBrowseRRsets(ctx, ListBrowseRRsetsParams{Zone: zone.ID, Generation: *zone.Generation, NameFrom: options.Name, NameThrough: upper, AfterName: afterName, AfterType: afterType})
		} else {
			filtered, queryErr := q.ListBrowseRRsetsByType(ctx, ListBrowseRRsetsByTypeParams{Zone: zone.ID, Generation: *zone.Generation, Type: options.Type, NameFrom: options.Name, NameThrough: upper, AfterName: afterName, AfterType: afterType})
			err = queryErr
			items = make([]ListBrowseRRsetsRow, len(filtered))
			for i, row := range filtered {
				items[i] = ListBrowseRRsetsRow(row)
			}
		}
		if err != nil {
			return BrowsePage{}, err
		}

		for _, item := range items[:min(100, len(items))] {
			result.Items = append(result.Items, item.Payload)
		}
		if len(items) > 100 {
			last := items[99]
			cursor, err := encodeBrowseCursor(state, options, last.Name, last.Type, time.Now().Add(15*time.Minute))
			if err != nil {
				return BrowsePage{}, err
			}
			result.NextCursor = &cursor
		}
	} else if options.Cursor != "" {
		return BrowsePage{}, ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return BrowsePage{}, err
	}
	return result, nil
}

// ErrBrowseLeaseLost prevents a replaced, expired or retired worker publishing.
var ErrBrowseLeaseLost = errors.New("database: browse lease lost")
