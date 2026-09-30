package hardware

import (
	"path/filepath"
	"strings"
	"testing"
)

func keys(a []Advice) string {
	var k []string
	for _, x := range a {
		k = append(k, x.Key)
	}
	return strings.Join(k, ",")
}

func TestAHealthyPodHasNothingToSay(t *testing.T) {
	s := LiveStats{MemTotalMB: 8000, MemAvailableMB: 5000, DiskFreeGB: 200, DiskTotalGB: 256, DiskFreePct: 78, Memory: PressureNormal, Storage: PressureNormal}
	if got := Assess(s, false, false); len(got) != 0 {
		t.Errorf("a healthy Pod must stay quiet, got %s", keys(got))
	}
}

func TestItSpeaksAtTheFirstWarningNotJustWhenCritical(t *testing.T) {
	s := LiveStats{MemTotalMB: 8000, MemAvailableMB: 900, DiskFreeGB: 20, DiskTotalGB: 256, DiskFreePct: 8, Memory: PressureWarning, Storage: PressureWarning}
	got := keys(Assess(s, false, false))
	if !strings.Contains(got, "storage-low") || !strings.Contains(got, "memory-low") {
		t.Errorf("warnings must be announced before things get slow, got %q", got)
	}
	for _, a := range Assess(s, false, false) {
		if a.Urgent {
			t.Errorf("an early warning must not be urgent: %s", a.Key)
		}
	}
}

func TestCriticalStorageIsUrgentAndSaysWhatToDo(t *testing.T) {
	s := LiveStats{MemTotalMB: 8000, MemAvailableMB: 5000, DiskFreeGB: 1, DiskTotalGB: 256, DiskFreePct: 1, Memory: PressureNormal, Storage: PressureCritical}
	a := Assess(s, false, false)
	if len(a) != 1 || !a[0].Urgent || !strings.Contains(a[0].Text, "Delete old downloads") {
		t.Errorf("critical storage must be urgent and actionable: %+v", a)
	}
}

func TestSustainedShortageBecomesAnUpgradeSignal(t *testing.T) {
	s := LiveStats{MemTotalMB: 4000, MemAvailableMB: 500, Memory: PressureWarning, Storage: PressureNormal, DiskTotalGB: 128, DiskFreeGB: 90, DiskFreePct: 70}
	if !strings.Contains(keys(Assess(s, false, true)), "memory-upgrade") {
		t.Error("memory that stays short must produce an upgrade recommendation")
	}
	if strings.Contains(keys(Assess(s, false, false)), "memory-upgrade") {
		t.Error("a brief dip must not")
	}
}

func TestUndersizedHardwareIsSaidOnceUpFront(t *testing.T) {
	s := LiveStats{MemTotalMB: 2000, MemAvailableMB: 1500, DiskTotalGB: 32, DiskFreeGB: 25, DiskFreePct: 78, Memory: PressureNormal, Storage: PressureNormal}
	got := keys(Assess(s, false, false))
	if !strings.Contains(got, "undersized-memory") || !strings.Contains(got, "undersized-storage") {
		t.Errorf("a too-small Pod must say so, got %q", got)
	}
	if strings.Contains(Assess(s, false, false)[0].Text, "Raspberry") {
		t.Error("advice must talk about the Pod, not a brand")
	}
}

func TestMemoryCardIsFlaggedAndUnknownIsNot(t *testing.T) {
	if !rootOnMemoryCard("/dev/mmcblk0p2 / ext4 rw 0 0\ntmpfs /run tmpfs rw 0 0\n") {
		t.Error("a card-backed root must be detected")
	}
	if rootOnMemoryCard("/dev/nvme0n1p2 / ext4 rw 0 0\n") || rootOnMemoryCard("") {
		t.Error("an SSD or an unknown root must not be flagged")
	}
	s := LiveStats{MemTotalMB: 8000, MemAvailableMB: 5000, DiskTotalGB: 256, DiskFreeGB: 200, DiskFreePct: 78}
	if !strings.Contains(keys(Assess(s, true, false)), "memory-card") {
		t.Error("a memory card must produce advice")
	}
}

func TestAnUncleanStopIsRememberedAndACleanOneIsNot(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state", "running")
	if MarkRunning(p) {
		t.Error("the first start has no earlier run to have been cut off")
	}
	if !MarkRunning(p) {
		t.Error("a start that finds the marker means the last run did not stop cleanly")
	}
	MarkStopped(p)
	if MarkRunning(p) {
		t.Error("after a clean stop the next start must not report a cut")
	}
}
