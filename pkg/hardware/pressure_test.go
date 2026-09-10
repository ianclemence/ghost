package hardware

import "testing"

func TestClassifyMemory(t *testing.T) {
	cases := []struct {
		total, avail int
		want         Pressure
	}{
		{8000, 4000, PressureNormal},
		{8000, 900, PressureWarning},  // ~11%
		{8000, 500, PressureCritical}, // ~6%
		{8000, 300, PressureCritical}, // absolute floor
		{0, 0, PressureNormal},        // unknown -> don't alarm
	}
	for _, c := range cases {
		if got := classifyMemory(c.total, c.avail); got != c.want {
			t.Fatalf("classifyMemory(%d,%d) = %s want %s", c.total, c.avail, got, c.want)
		}
	}
}

func TestClassifyStorage(t *testing.T) {
	cases := []struct {
		freeGB, freePct int
		want            Pressure
	}{
		{100, 60, PressureNormal},
		{5, 12, PressureWarning},
		{2, 5, PressureCritical},
		{0, 0, PressureCritical}, // no free space is critical
	}
	for _, c := range cases {
		if got := classifyStorage(c.freeGB, c.freePct); got != c.want {
			t.Fatalf("classifyStorage(%d,%d) = %s want %s", c.freeGB, c.freePct, got, c.want)
		}
	}
}

func TestContextScale(t *testing.T) {
	if got := (LiveStats{Memory: PressureNormal, Storage: PressureNormal}).ContextScale(); got != 1.0 {
		t.Fatalf("normal scale = %v", got)
	}
	if got := (LiveStats{Memory: PressureWarning, Storage: PressureNormal}).ContextScale(); got != 0.5 {
		t.Fatalf("warning scale = %v", got)
	}
	// The worst of the two pressures wins.
	if got := (LiveStats{Memory: PressureNormal, Storage: PressureCritical}).ContextScale(); got != 0.25 {
		t.Fatalf("critical scale = %v", got)
	}
}

func TestSnapshotDoesNotPanic(t *testing.T) {
	s := Snapshot("/")
	if s.Memory == "" || s.Storage == "" {
		t.Fatalf("snapshot must classify both pressures: %+v", s)
	}
	if s.Worst() == "" {
		t.Fatal("worst pressure must be set")
	}
}
