package execplan

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/effort"
)

func TestLocalOnlyNeverCloud(t *testing.T) {
	in := Input{Effort: effort.Deep, Privacy: PrivacyLocalOnly,
		Avail: Availability{Phone: true, PhoneModel: true, Pod: true, PodModel: true, Cloud: true, CloudModel: true}}
	if d := Plan(in); d.Target == TargetCloud {
		t.Fatalf("local-only must never select cloud: %+v", d)
	}
}

func TestQuickPrefersPhone(t *testing.T) {
	in := Input{Effort: effort.Quick, Privacy: PrivacyBalanced,
		Avail: Availability{Phone: true, PhoneModel: true, Pod: true, PodModel: true}}
	if d := Plan(in); d.Target != TargetPhone {
		t.Fatalf("want phone, got %+v", d)
	}
}

func TestDeepPrefersPod(t *testing.T) {
	in := Input{Effort: effort.Deep, Privacy: PrivacyBalanced,
		Avail: Availability{Phone: true, PhoneModel: true, Pod: true, PodModel: true}}
	if d := Plan(in); d.Target != TargetPod {
		t.Fatalf("want pod, got %+v", d)
	}
}

func TestHardwareRoutesToPod(t *testing.T) {
	in := Input{Effort: effort.Normal, Privacy: PrivacyBalanced,
		Avail: Availability{Phone: true, PhoneModel: true, Pod: true, PodModel: true, NeedsHardware: true, PodHasHardware: true}}
	if d := Plan(in); d.Target != TargetPod {
		t.Fatalf("want pod for hardware, got %+v", d)
	}
}

func TestDeterministic(t *testing.T) {
	in := Input{Effort: effort.Normal, Privacy: PrivacyBalanced,
		Avail: Availability{Phone: true, PhoneModel: true}}
	if Plan(in) != Plan(in) {
		t.Fatal("planner must be deterministic")
	}
}
