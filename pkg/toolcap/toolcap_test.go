package toolcap

import "testing"

func TestPlacementDrivesRouting(t *testing.T) {
	tools := []Definition{
		{Name: "calendar.create", Executors: []Executor{ExecPhone}},
		{Name: "hardware.lamp.set", Executors: []Executor{ExecPod}},
	}
	ad := Advertisement{DeviceID: "pod", Tools: tools}
	got := ad.AvailableOn(ExecPod)
	if len(got) != 1 || got[0] != "hardware.lamp.set" {
		t.Fatalf("pod availability wrong: %v", got)
	}
	hw := PodHardwareTools(tools)
	if len(hw) != 1 || hw[0] != "hardware.lamp.set" {
		t.Fatalf("pod-hardware derivation wrong: %v", hw)
	}
}
