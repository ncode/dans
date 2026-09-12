package database

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestBrowseCursorBindsSnapshotAndFilters(t *testing.T) {
	t.Parallel()
	state := browseSnapshot{ID: "5cf7be7e-410b-4b9c-ae37-72d4b7d3a227", Generation: "6cf7be7e-410b-4b9c-ae37-72d4b7d3a227", Revision: 3, CursorKey: bytes.Repeat([]byte{7}, 32)}
	options := BrowseOptions{Name: "www.", Match: "prefix", Type: "A"}
	cursor, err := encodeBrowseCursor(state, options, "www.example.test.", "A", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	name, typ, err := decodeBrowseCursor(cursor, state, options)
	if err != nil || name != "www.example.test." || typ != "A" {
		t.Fatalf("decoded = %q %q %v", name, typ, err)
	}
	for _, mutation := range []string{"altered", "oversized", "zone", "generation", "revision", "filter", "expired"} {
		t.Run(mutation, func(t *testing.T) {
			s, o, c := state, options, cursor
			switch mutation {
			case "altered":
				c = "A" + c[1:]
			case "oversized":
				c = strings.Repeat("A", 4097)
			case "zone":
				s.ID = "other"
			case "generation":
				s.Generation = "other"
			case "revision":
				s.Revision++
			case "filter":
				o.Type = "TXT"
			case "expired":
				c, _ = encodeBrowseCursor(s, o, name, typ, time.Now().Add(-time.Hour))
			}
			if _, _, err := decodeBrowseCursor(c, s, o); err == nil {
				t.Fatal("accepted incompatible cursor")
			}
		})
	}
}

func TestNormalizeBrowseFilters(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input    BrowseOptions
		wantName string
		fail     bool
	}{
		{BrowseOptions{Name: "WWW.Example.Test", Match: "exact"}, "www.example.test.", false},
		{BrowseOptions{Name: "WWW.Exa", Match: "prefix"}, "www.exa", false},
		{BrowseOptions{Name: "_sip._tcp", Type: "SRV"}, "_sip._tcp", false},
		{BrowseOptions{Name: "%", Match: "prefix"}, "%", false},
		{BrowseOptions{Match: "contains"}, "", true},
		{BrowseOptions{Type: "A OR 1=1"}, "", true},
	} {
		got, err := normalizeBrowseOptions(tc.input)
		if (err != nil) != tc.fail || !tc.fail && got.Name != tc.wantName {
			t.Errorf("normalize %+v = %+v, %v", tc.input, got, err)
		}
	}
}

func TestBrowseCanonicalPayloadValidation(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"name":"example.test.","type":"A","records":[],"ttl":-1}`,
		`{"name":"example.test.","type":"A","ttl":60}`,
		`{"name":"example.test.","type":"A","records":[]}`,
		`{"name":"outside.invalid","type":"A","records":[],"ttl":60}`,
	} {
		if _, err := canonicalBrowseSet([]byte(raw)); err == nil {
			t.Errorf("accepted incomplete RRset: %s", raw)
		}
	}
}
