package personalcontext

import (
	"testing"
)

// BenchmarkCurrent guards the hot read every turn performs: assembling
// the current context from the entry log. Regression here slows all
// responses, so the number is tracked, not just asserted.
func BenchmarkCurrent(b *testing.B) {
	s, err := Open(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		e := mkEntry(idFor(b, i), "user", predFor(i), "value")
		if _, err := s.Create(e); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if cur := s.Current(); len(cur) != 200 {
			b.Fatalf("got %d entries", len(cur))
		}
	}
}

func idFor(b *testing.B, i int) string {
	b.Helper()
	return "bench-" + itoa(i)
}

func predFor(i int) string {
	return "predicate_" + itoa(i%20)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [8]byte
	p := len(buf)
	for i > 0 {
		p--
		buf[p] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[p:])
}
