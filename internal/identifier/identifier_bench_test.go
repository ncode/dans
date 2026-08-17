package identifier

import (
	"crypto/sha256"
	"testing"
)

const benchmarkToken = TokenPrefix + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func BenchmarkValidateToken(b *testing.B) {
	b.ReportAllocs()
	var err error
	for b.Loop() {
		err = ValidateToken(benchmarkToken)
	}
	if err != nil {
		b.Fatalf("ValidateToken(%q): %v", benchmarkToken, err)
	}
}

func BenchmarkDigestToken(b *testing.B) {
	want := sha256.Sum256([]byte(benchmarkToken))
	b.ReportAllocs()
	var digest [32]byte
	for b.Loop() {
		digest = DigestToken(benchmarkToken)
	}
	if digest != want {
		b.Fatalf("DigestToken() = %x, want %x", digest, want)
	}
}
