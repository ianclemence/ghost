package hardware

import (
	"os"
	"strconv"
	"strings"
)

// Pressure classifies resource headroom. Ghost should degrade gracefully
// rather than collapse: under pressure it retrieves less, caches less, and
// avoids loading extra models — while never touching canonical data.
type Pressure string

const (
	PressureNormal   Pressure = "normal"
	PressureWarning  Pressure = "warning"
	PressureCritical Pressure = "critical"
)

// LiveStats is a point-in-time resource snapshot for Doctor and for adaptive
// budgeting.
type LiveStats struct {
	MemTotalMB     int      `json:"mem_total_mb"`
	MemAvailableMB int      `json:"mem_available_mb"`
	DiskFreeGB     int      `json:"disk_free_gb"`
	DiskTotalGB    int      `json:"disk_total_gb"`
	DiskFreePct    int      `json:"disk_free_pct"`
	Memory         Pressure `json:"memory"`
	Storage        Pressure `json:"storage"`
}

// Snapshot reads live memory and disk headroom for the filesystem holding
// path. Missing inputs degrade to "normal" rather than alarming falsely.
func Snapshot(path string) LiveStats {
	var s LiveStats
	s.MemTotalMB, s.MemAvailableMB = memInfoMB()
	s.DiskFreeGB, s.DiskTotalGB, s.DiskFreePct = diskInfoGB(path)

	s.Memory = classifyMemory(s.MemTotalMB, s.MemAvailableMB)
	s.Storage = classifyStorage(s.DiskFreeGB, s.DiskFreePct)
	return s
}

// classifyMemory: warn below 15% available or 700MB; critical below 8% or 350MB.
// Thresholds are absolute + relative so both small and large boards are covered.
func classifyMemory(totalMB, availMB int) Pressure {
	if totalMB <= 0 {
		return PressureNormal
	}
	pct := availMB * 100 / totalMB
	switch {
	case pct < 8 || availMB < 350:
		return PressureCritical
	case pct < 15 || availMB < 700:
		return PressureWarning
	default:
		return PressureNormal
	}
}

// classifyStorage: warn below 15% free or 3GB; critical below 7% or 1GB.
func classifyStorage(freeGB, freePct int) Pressure {
	switch {
	case freePct < 7 || freeGB < 1:
		return PressureCritical
	case freePct < 15 || freeGB < 3:
		return PressureWarning
	default:
		return PressureNormal
	}
}

// ContextScale maps overall pressure to a multiplier for retrieval/context
// budgets: 1.0 normal, 0.5 warning, 0.25 critical. Canonical memory is never
// scaled away; only derived context is.
func (s LiveStats) ContextScale() float64 {
	worst := s.Memory
	if rank(s.Storage) > rank(worst) {
		worst = s.Storage
	}
	switch worst {
	case PressureCritical:
		return 0.25
	case PressureWarning:
		return 0.5
	default:
		return 1.0
	}
}

// Worst returns the more severe of the two pressures.
func (s LiveStats) Worst() Pressure {
	if rank(s.Storage) > rank(s.Memory) {
		return s.Storage
	}
	return s.Memory
}

func rank(p Pressure) int {
	switch p {
	case PressureCritical:
		return 2
	case PressureWarning:
		return 1
	default:
		return 0
	}
}

// memInfoMB parses /proc/meminfo for total and available memory. Returns
// (0,0) on non-Linux or unreadable input.
func memInfoMB() (int, int) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	var total, avail int
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			total = parseKB(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			avail = parseKB(line)
		}
	}
	return total / 1024, avail / 1024
}

func parseKB(line string) int {
	for _, f := range strings.Fields(line) {
		if n, err := strconv.Atoi(f); err == nil {
			return n
		}
	}
	return 0
}
