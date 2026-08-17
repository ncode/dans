package dnsname

import (
	"strconv"
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
						break
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
