package tools

import (
	"strings"
	"testing"
)

// The status tool must answer the questions it exists for — including the
// free-disk number a file-reading subagent could never get from /proc.
func TestSystemStatusAnswersDiskAndHealth(t *testing.T) {
	s := readSystemStatus()

	if s.MemTotalBytes == 0 {
		t.Error("memory total unreadable")
	}
	if s.MemAvailBytes > s.MemTotalBytes {
		t.Errorf("available memory %d exceeds total %d", s.MemAvailBytes, s.MemTotalBytes)
	}
	if s.CPUCount == 0 {
		t.Error("cpu count must be reported")
	}
	if s.UptimeSeconds <= 0 {
		t.Error("uptime must be positive")
	}
	if s.Load1 < 0 || s.Load5 < 0 || s.Load15 < 0 {
		t.Errorf("load averages must not be negative: %.2f %.2f %.2f", s.Load1, s.Load5, s.Load15)
	}
	if s.CPUTempC != nil && (*s.CPUTempC < -50 || *s.CPUTempC > 150) {
		t.Errorf("implausible CPU temperature: %.1f", *s.CPUTempC)
	}

	if len(s.Filesystems) == 0 {
		t.Fatal("no filesystems reported — free disk space would be unanswerable")
	}
	var root *statusFilesystem
	for i := range s.Filesystems {
		f := &s.Filesystems[i]
		if f.Mount == "/" {
			root = f
		}
		if f.TotalBytes == 0 {
			t.Errorf("filesystem %s reports zero total", f.Mount)
		}
		if f.FreeBytes == 0 && f.UsedBytes == 0 {
			t.Errorf("filesystem %s reports zero used AND zero free", f.Mount)
		}
	}
	if root == nil {
		t.Error("root filesystem missing from the report")
	} else if root.FreeBytes == 0 {
		t.Error("root filesystem free space is zero — real free space must be reported")
	}
}

func TestFormatSystemStatusShowsFreeSpace(t *testing.T) {
	temp := 47.4
	s := systemStatus{
		CPUTempC:      &temp,
		MemTotalBytes: 8 * 1024 * 1024 * 1024,
		MemAvailBytes: 6900 * 1024 * 1024,
		CPUCount:      4,
		Load1:         0.75,
		Load5:         1.07,
		Load15:        0.5,
		UptimeSeconds: 321.9,
		Filesystems: []statusFilesystem{{
			Device: "/dev/mmcblk0p2", Mount: "/", Type: "ext4",
			TotalBytes: 292 * 1024 * 1024 * 1024,
			UsedBytes:  210 * 1024 * 1024 * 1024,
			FreeBytes:  82 * 1024 * 1024 * 1024,
		}},
	}
	out := formatSystemStatus(s)
	for _, want := range []string{
		"CPU temperature: 47.4 °C",
		"6.7 GiB available", // formatted, exact-ish
		"Load average: 0.75 1.07 0.50 over 4 CPUs",
		"Disk / (",
		"82.0 GiB free",
		"(72% used)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("formatted status missing %q:\n%s", want, out)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[uint64]string{
		0:                        "0 B",
		512:                      "512 B",
		1024:                     "1.0 KiB",
		1024 * 1024:              "1.0 MiB",
		292 * 1024 * 1024 * 1024: "292.0 GiB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

// The offer path is intent-gated: a registered tool that never reaches the
// model's offered list is a question Ghost will refuse to answer. Status
// phrasings vary too much to keyword-match, so system_status must be in the
// always-offered core set — verified through the real turn filter.
func TestSystemStatusIsOfferedEveryTurn(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(NewSystemStatusTool())
	reg.Register(testTool{name: "web_search"})

	phrasings := []string{
		"check the free disk space",
		"what is the status of the device",
		"how much space is left",
		"is the machine healthy",
		"what's the temperature",
		"hello",
	}
	for _, msg := range phrasings {
		for _, profile := range []ToolProfile{ProfileFull, ProfileMobileSafe, ProfileMinimal} {
			offered := FilterToolsForTurn(reg, profile, msg, false)
			if _, ok := offered.Get("system_status"); !ok {
				t.Errorf("system_status not offered (profile=%q msg=%q)", profile, msg)
			}
		}
	}
	if !coreToolNames["system_status"] {
		t.Error("system_status must be core — keyword gating status questions loses")
	}
}
