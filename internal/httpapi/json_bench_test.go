package httpapi

import (
	"bytes"
	"encoding/json"
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

func zonePatchBenchmarkPayload(b *testing.B, size int) []byte {
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
