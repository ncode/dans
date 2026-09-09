package dnsname

import (
	"strconv"
	"strings"
	"testing"
)

func BenchmarkCanonicalizeRRsetBatch(b *testing.B) {
	for _, size := range []int{1, 100} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			owners := make([]string, size)
			for i := range owners {
				owners[i] = "WWW.Service.Example.COM."
			}

			b.ReportAllocs()
			var (
				canonicalized   int
				literalWildcard bool
				last            Name
				err             error
			)
			for b.Loop() {
				canonicalized = 0
				for _, owner := range owners {
					last, err = Parse(owner)
					if err != nil {
						b.Fatal(err)
					}
					literalWildcard = last.HasLiteralWildcard()
					canonicalized++
				}
			}
			if err != nil || canonicalized != len(owners) || literalWildcard || last.String() != "www.service.example.com." {
				b.Fatalf("canonicalized %d/%d owners: last=%q err=%v", canonicalized, len(owners), last, err)
			}
		})
	}
}

func BenchmarkCanonicalizeOwnerVariants(b *testing.B) {
	for _, test := range []struct{ name, owner, want string }{
		{"Unicode", "BÜCHER.example.", "xn--bcher-kva.example."},
		{"Escaped", `foo\046bar.Example.`, `foo\046bar.example.`},
		{"Long", strings.Repeat("A", 63) + "." + strings.Repeat("b.", 94), strings.Repeat("a", 63) + "." + strings.Repeat("b.", 94)},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			var got Name
			for b.Loop() {
				var err error
				got, err = Parse(test.owner)
				if err != nil {
					b.Fatal(err)
				}
			}
			if got.String() != test.want {
				b.Fatalf("canonical owner = %q, want %q", got, test.want)
			}
		})
	}
}
