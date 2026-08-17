package delegation

import (
	"errors"
	"testing"

	"github.com/ncode/dans/internal/dnsname"
)

func TestCompileSelectorCanonicalizesAndCompilesSQLLike(t *testing.T) {
	t.Parallel()

	zone := mustName(t, "example.com.")
	tests := []struct {
		name        string
		kind        SelectorKind
		pattern     string
		wantPattern string
		wantLike    string
	}{
		{
			name:        "exact",
			kind:        SelectorExact,
			pattern:     "WWW.Example.COM.",
			wantPattern: "www.example.com.",
			wantLike:    "www.example.com.",
		},
		{
			name:        "glob",
			kind:        SelectorGlob,
			pattern:     "API.*.Example.COM.",
			wantPattern: "api.*.example.com.",
			wantLike:    "api.%.example.com.",
		},
		{
			name:        "question",
			kind:        SelectorGlob,
			pattern:     "api.?.example.com.",
			wantPattern: "api.?.example.com.",
			wantLike:    "api._.example.com.",
		},
		{
			name:        "SQL metacharacters are literal",
			kind:        SelectorGlob,
			pattern:     `foo%_\092bar*.example.com.`,
			wantPattern: `foo%_\092bar*.example.com.`,
			wantLike:    `foo\%\_\\092bar%.example.com.`,
		},
		{
			name:        "escaped wildcard is literal",
			kind:        SelectorGlob,
			pattern:     `host\042.example.com.`,
			wantPattern: `host\042.example.com.`,
			wantLike:    `host\\042.example.com.`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			selector, err := CompileSelector(tt.kind, tt.pattern, zone)
			if err != nil {
				t.Fatalf("CompileSelector(%q, %q): %v", tt.kind, tt.pattern, err)
			}
			if got := selector.Pattern(); got != tt.wantPattern {
				t.Errorf("Pattern() = %q, want %q", got, tt.wantPattern)
			}
			if got := selector.SQLLike(); got != tt.wantLike {
				t.Errorf("SQLLike() = %q, want %q", got, tt.wantLike)
			}
		})
	}
}

func TestCompileSelectorRejectsInvalidOrOutOfZonePatterns(t *testing.T) {
	t.Parallel()

	zone := mustName(t, "example.com.")
	tests := []struct {
		name    string
		kind    SelectorKind
		pattern string
	}{
		{name: "unknown kind", kind: "regex", pattern: "www.example.com."},
		{name: "relative", kind: SelectorGlob, pattern: "*.example.com"},
		{name: "outside zone", kind: SelectorGlob, pattern: "*.example.net."},
		{name: "text suffix lacks label boundary", kind: SelectorGlob, pattern: "*example.com."},
		{name: "empty label", kind: SelectorGlob, pattern: "*..example.com."},
		{name: "non-ASCII mixed with metacharacter", kind: SelectorGlob, pattern: "b\u00fc*.example.com."},
		{name: "escaped non-ASCII mixed with metacharacter", kind: SelectorGlob, pattern: "b\\\u00fc*.example.com."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := CompileSelector(tt.kind, tt.pattern, zone); !errors.Is(err, ErrInvalidSelector) {
				t.Errorf("CompileSelector(%q, %q) error = %v, want ErrInvalidSelector", tt.kind, tt.pattern, err)
			}
		})
	}
}

func TestSelectorMatchesWholeCanonicalName(t *testing.T) {
	t.Parallel()

	zone := mustName(t, "example.com.")
	tests := []struct {
		name    string
		pattern string
		owner   string
		want    bool
	}{
		{name: "star one label", pattern: "*.apps.example.com.", owner: "one.apps.example.com.", want: true},
		{name: "star crosses labels", pattern: "*.apps.example.com.", owner: "one.two.apps.example.com.", want: true},
		{name: "anchored at start", pattern: "api.*.example.com.", owner: "prefix.api.one.example.com.", want: false},
		{name: "anchored at end", pattern: "api.*.example.com.", owner: "api.one.example.com.invalid.", want: false},
		{name: "question one character", pattern: "api.?.example.com.", owner: "api.x.example.com.", want: true},
		{name: "question not two characters", pattern: "api.?.example.com.", owner: "api.xy.example.com.", want: false},
		{name: "percent is literal", pattern: "foo%.example.com.", owner: "foox.example.com.", want: false},
		{name: "underscore is literal", pattern: "foo_.example.com.", owner: "foox.example.com.", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			selector, err := CompileSelector(SelectorGlob, tt.pattern, zone)
			if err != nil {
				t.Fatalf("CompileSelector(%q): %v", tt.pattern, err)
			}
			owner := mustName(t, tt.owner)
			if got := selector.Matches(owner); got != tt.want {
				t.Errorf("selector %q Matches(%q) = %t, want %t", tt.pattern, tt.owner, got, tt.want)
			}
		})
	}
}

func TestSelectorProtectsApexAndLiteralWildcardFromGlobs(t *testing.T) {
	t.Parallel()

	zone := mustName(t, "example.com.")
	glob, err := CompileSelector(SelectorGlob, "*.example.com.", zone)
	if err != nil {
		t.Fatalf("CompileSelector glob: %v", err)
	}
	if glob.Matches(zone) {
		t.Error("glob authorized zone apex")
	}
	wildcard := mustName(t, "*.example.com.")
	if glob.Matches(wildcard) {
		t.Error("glob authorized literal wildcard RRset")
	}

	exactApex, err := CompileSelector(SelectorExact, "example.com.", zone)
	if err != nil {
		t.Fatalf("CompileSelector exact apex: %v", err)
	}
	if !exactApex.Matches(zone) {
		t.Error("exact apex selector did not authorize apex")
	}
	exactWildcard, err := CompileSelector(SelectorExact, "*.example.com.", zone)
	if err != nil {
		t.Fatalf("CompileSelector exact wildcard: %v", err)
	}
	if !exactWildcard.Matches(wildcard) {
		t.Error("exact wildcard selector did not authorize literal wildcard RRset")
	}
}

func mustName(t *testing.T, input string) dnsname.Name {
	t.Helper()
	name, err := dnsname.Parse(input)
	if err != nil {
		t.Fatalf("dnsname.Parse(%q): %v", input, err)
	}
	return name
}
