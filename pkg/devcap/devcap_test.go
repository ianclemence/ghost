package devcap

import "testing"

func TestEvaluateTiers(t *testing.T) {
	good := Device{Platform: "android", Arch: "arm64", TotalRAMMB: 8192, FreeDiskMB: 14000, Accelerator: "nnapi", Runtime: "mobile-local"}
	need := ModelNeed{ModelID: "ghost-mini-1", Runtime: "mobile-local", Platforms: []string{"android"}, Archs: []string{"arm64"}, MinRAMMB: 4000, RecRAMMB: 6000, SizeMB: 2500}
	if r := Evaluate(good, need); r.Verdict != Compatible {
		t.Fatalf("want compatible, got %+v", r)
	}
	weak := good
	weak.TotalRAMMB = 5000
	if r := Evaluate(weak, need); r.Verdict != CompatibleNotRecommended {
		t.Fatalf("want compatible_not_recommended, got %+v", r)
	}
	tiny := good
	tiny.TotalRAMMB = 2048
	if r := Evaluate(tiny, need); r.Verdict != Incompatible {
		t.Fatalf("want incompatible, got %+v", r)
	}
	hot := good
	hot.ThermalState = "critical"
	if r := Evaluate(hot, need); r.Verdict != TemporarilyUnavailable {
		t.Fatalf("want temporarily_unavailable, got %+v", r)
	}
	pressure := good
	pressure.LowMemory = true
	if r := Evaluate(pressure, need); r.Verdict != TemporarilyUnavailable {
		t.Fatalf("want temporarily_unavailable, got %+v", r)
	}
	wrongOS := good
	wrongOS.Platform = "ios"
	if r := Evaluate(wrongOS, need); r.Verdict != Incompatible {
		t.Fatalf("want incompatible for platform, got %+v", r)
	}
}
