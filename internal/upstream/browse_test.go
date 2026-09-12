package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestStreamRRsetsCompleteAndRejectPartial(t *testing.T) {
	t.Parallel()
	good := `{"id":"example.test.","name":"example.test.","rrsets":[{"name":"www.example.test.","type":"A","ttl":60,"records":[{"content":"192.0.2.1","disabled":false},{"content":"192.0.2.2","disabled":true}],"comments":[{"content":"two values","account":"","modified_at":1}]}]}`
	for _, tc := range []struct {
		name, body string
		fail       bool
	}{{"complete", good, false}, {"truncated", good[:len(good)-2], true}, {"missing array", `{"id":"example.test."}`, true}, {"duplicate array", `{"rrsets":[],"rrsets":[]}`, true}, {"trailing", good + `{}`, true}} {
		t.Run(tc.name, func(t *testing.T) {
			var sets []json.RawMessage
			err := streamRRsets(t.Context(), strings.NewReader(tc.body), func(raw json.RawMessage) error { sets = append(sets, raw); return nil })
			if (err != nil) != tc.fail {
				t.Fatalf("stream error = %v, want failure %v", err, tc.fail)
			}
			if !tc.fail && (len(sets) != 1 || !strings.Contains(string(sets[0]), "192.0.2.2")) {
				t.Fatalf("incomplete RRset: %s", sets)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := streamRRsets(ctx, strings.NewReader(good), func(json.RawMessage) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
}
