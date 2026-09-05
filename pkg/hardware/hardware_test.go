package hardware

import (
	"testing"
)

func TestDetectSane(t *testing.T) {
	p := Detect()
	if p.Arch == "" || p.OS == "" || p.Cores <= 0 || p.RAMGB <= 0 {
		t.Fatalf("profile must be populated: %+v", p)
	}
	if p.Class == "" {
		t.Fatal("class must resolve")
	}
	d := DefaultsFor(p)
	if d.MaxConcurrency <= 0 || d.ContextTokens <= 0 || d.MemoryBudgetMB <= 0 {
		t.Fatalf("defaults must be positive: %+v", d)
	}
}

func TestClassDefaultsMonotonic(t *testing.T) {
	// Stronger hardware never gets weaker budgets.
	order := []Class{ClassPi5, ClassRK1, ClassMiniPC, ClassGPUServer}
	prev := 0
	for _, c := range order {
		d := DefaultsFor(Profile{Class: c})
		if d.MemoryBudgetMB < prev {
			t.Fatalf("class %s regressed budget", c)
		}
		prev = d.MemoryBudgetMB
	}
}

func TestClassify(t *testing.T) {
	if classify(Profile{Arch: "arm64", RAMGB: 16}) != ClassRK1 {
		t.Fatal("16GB ARM must be RK1-class")
	}
	if classify(Profile{Arch: "amd64", RAMGB: 8, Accel: AccelNone}) != ClassMiniPC {
		t.Fatal("x86 must be mini-PC")
	}
	if classify(Profile{Arch: "amd64", Accel: AccelCUDA}) != ClassGPUServer {
		t.Fatal("CUDA must be server-class")
	}
}

func TestNoVendorAssumptions(t *testing.T) {
	// Detection must not fail on unknown machines — generic fallback.
	p := Profile{Arch: "riscv64", RAMGB: 2}
	if classify(p) != ClassGeneric {
		t.Fatal("unknown arch must fall back to generic")
	}
	d := DefaultsFor(p)
	if d.ModelTier != "small" {
		t.Fatal("generic must default small")
	}
}
