package credentials

import (
	"testing"
)

// BenchmarkCredentialReferenceLookup measures the safe metadata reference
// path (the worker-facing credential_ref lookup). It never touches or
// exposes the secret value.
func BenchmarkCredentialReferenceLookup(b *testing.B) {
	v := New("")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if got := v.Ref("google-calendar"); got.ID != "google-calendar" {
			b.Fatal("bad ref")
		}
	}
}
