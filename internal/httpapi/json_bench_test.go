package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/ncode/dans/api"
)

func BenchmarkStrictJSONZonePatch(b *testing.B) {
	for _, size := range []int{1, 100} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			payload := zonePatchBenchmarkPayload(b, size)
			var decoded api.ZonePatch
			next := func(value api.ZonePatch) error {
				decoded = value
				return nil
			}

			b.ReportAllocs()
			b.SetBytes(int64(len(payload)))
			var err error
			for b.Loop() {
				err = StrictJSON(bytes.NewReader(payload), 1<<20, next)
				if err != nil {
					b.Fatal(err)
				}
			}
			if err != nil || len(decoded.Rrsets) != size {
				b.Fatalf("StrictJSON decoded %d/%d RRsets: %v", len(decoded.Rrsets), size, err)
			}
			first := decoded.Rrsets[0]
			last := decoded.Rrsets[len(decoded.Rrsets)-1]
			if first.Name != "host-0.example.org." || last.Name != "host-"+strconv.Itoa(size-1)+".example.org." ||
				first.Type != "A" || first.Changetype != "REPLACE" || first.Ttl == nil || *first.Ttl != 300 ||
				first.Records == nil || len(*first.Records) != 1 || (*first.Records)[0].Content != "192.0.2.1" {
				b.Fatalf("StrictJSON returned unexpected first/last RRsets: first=%+v last=%+v", first, last)
			}
		})
	}
}

func zonePatchBenchmarkPayload(b testing.TB, size int) []byte {
	b.Helper()
	patch := api.ZonePatch{Rrsets: make([]api.RRSetChange, size)}
	for i := range patch.Rrsets {
		disabled := false
		ttl := 300
		records := []api.Record{{Content: "192.0.2.1", Disabled: &disabled}}
		patch.Rrsets[i] = api.RRSetChange{
			Changetype: "REPLACE",
			Name:       "host-" + strconv.Itoa(i) + ".example.org.",
			Records:    &records,
			Ttl:        &ttl,
			Type:       "A",
		}
	}
	payload, err := json.Marshal(patch)
	if err != nil {
		b.Fatalf("marshal ZonePatch benchmark payload: %v", err)
	}
	return payload
}

func BenchmarkStrictJSONVariants(b *testing.B) {
	base := zonePatchBenchmarkPayload(b, 100)
	var patch api.ZonePatch
	if err := json.Unmarshal(base, &patch); err != nil {
		b.Fatal(err)
	}
	for i := range patch.Rrsets {
		record := (*patch.Rrsets[i].Records)[0]
		*patch.Rrsets[i].Records = make([]api.Record, 32)
		for j := range *patch.Rrsets[i].Records {
			(*patch.Rrsets[i].Records)[j] = record
		}
	}
	large, err := json.Marshal(patch)
	if err != nil {
		b.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		payload []byte
		status  int
	}{
		{"Records32", large, 0},
		{"LateDuplicate", append(bytes.Clone(base[:len(base)-1]), []byte(`,"rrsets":[]}`)...), http.StatusBadRequest},
		{"LateUnknown", append(bytes.Clone(base[:len(base)-1]), []byte(`,"unexpected":true}`)...), http.StatusUnprocessableEntity},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(test.payload)))
			var decoded api.ZonePatch
			calls := 0
			next := func(value api.ZonePatch) error { decoded = value; calls++; return nil }
			for b.Loop() {
				err := StrictJSON(bytes.NewReader(test.payload), 1<<20, next)
				if test.status == 0 && err != nil || test.status != 0 && (err == nil || StatusCode(err) != test.status) {
					b.Fatalf("decode error = %v, want status %d", err, test.status)
				}
			}
			if test.status != 0 {
				if calls != 0 {
					b.Fatal("invalid payload reached callback")
				}
			} else if calls != b.N || len(decoded.Rrsets) != 100 || len(*decoded.Rrsets[99].Records) != 32 || (*decoded.Rrsets[99].Records)[31].Content != "192.0.2.1" {
				b.Fatal("large payload decoded incorrectly")
			}
		})
	}
}
