package dnsname

import (
	"errors"
	"strings"
	"testing"
)

func TestParseCanonicalizesAbsoluteNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "root", input: ".", want: "."},
		{name: "ascii case", input: "WWW.Example.COM.", want: "www.example.com."},
		{name: "IDNA2008", input: "b\u00fccher.example.", want: "xn--bcher-kva.example."},
		{name: "non-hostname labels", input: "_SIP._TCP.Example.", want: "_sip._tcp.example."},
		{name: "escaped dot", input: `foo\046bar.Example.`, want: `foo\046bar.example.`},
		{name: "short escaped dot", input: `foo\.bar.Example.`, want: `foo\046bar.example.`},
		{name: "escaped octet", input: `key\255.Example.`, want: `key\255.example.`},
		{name: "escaped ASCII is folded", input: `\087WW.Example.`, want: "www.example."},
		{name: "literal wildcard label", input: `\042.Example.`, want: "*.example."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.input, err)
			}
			if got.String() != tt.want {
				t.Errorf("Parse(%q) = %q, want %q", tt.input, got.String(), tt.want)
			}
		})
	}
}

func TestParseRejectsInvalidNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "relative", input: "www.example.com"},
		{name: "empty label", input: "www..example."},
		{name: "dangling escape", input: "www\\"},
		{name: "bad decimal escape", input: `www\999.example.`},
		{name: "label too long", input: strings.Repeat("a", 64) + ".example."},
		{name: "name too long", input: strings.Repeat("a.", 127) + "a."},
		{name: "invalid IDNA", input: "a\u200d.example."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(tt.input); !errors.Is(err, ErrInvalidName) {
				t.Errorf("Parse(%q) error = %v, want ErrInvalidName", tt.input, err)
			}
		})
	}
}

func TestNameWithinUsesLabelBoundaries(t *testing.T) {
	t.Parallel()

	zone := mustParse(t, "example.com.")
	tests := []struct {
		owner string
		want  bool
	}{
		{owner: "example.com.", want: true},
		{owner: "www.example.com.", want: true},
		{owner: "one.two.example.com.", want: true},
		{owner: "badexample.com.", want: false},
		{owner: "example.net.", want: false},
		{owner: "com.", want: false},
		{owner: ".", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.owner, func(t *testing.T) {
			t.Parallel()
			owner := mustParse(t, tt.owner)
			if got := owner.Within(zone); got != tt.want {
				t.Errorf("%q.Within(%q) = %t, want %t", owner, zone, got, tt.want)
			}
		})
	}
}

func TestNameHasLiteralWildcard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		owner string
		want  bool
	}{
		{owner: "*.example.com.", want: true},
		{owner: "foo.*.example.com.", want: true},
		{owner: "foo*.example.com.", want: false},
		{owner: "www.example.com.", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.owner, func(t *testing.T) {
			t.Parallel()
			owner := mustParse(t, tt.owner)
			if got := owner.HasLiteralWildcard(); got != tt.want {
				t.Errorf("%q.HasLiteralWildcard() = %t, want %t", owner, got, tt.want)
			}
		})
	}
}

func mustParse(t *testing.T, input string) Name {
	t.Helper()
	name, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(%q): %v", input, err)
	}
	return name
}
