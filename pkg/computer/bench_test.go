package computer

import (
	"context"
	"testing"
)

// BenchmarkComputerInspectUI measures the cost of the bounded semantic UI
// observation primitive (the cc-01 backbone) on the deterministic fixture.
func BenchmarkComputerInspectUI(b *testing.B) {
	v := NewVirtualUI("bench")
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := v.Do(ctx, OpInspectUI, nil); err != nil {
			b.Fatal(err)
		}
	}
}
