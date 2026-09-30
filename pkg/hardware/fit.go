package hardware

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// What a Pod needs to do its job well. Below these it still runs, and says so.
const (
	MinMemoryMB = 3500 // a 4 GB Pod reports a little under 4096
	MinDiskGB   = 55   // a 64 GB drive reports a little under 64
)

// Advice is something the Pod should tell its owner about itself: what is
// wrong, and what to do about it, in words a person can act on.
type Advice struct {
	Key      string
	Text     string
	Urgent   bool
	Cooldown time.Duration
}

// OnMemoryCard reports whether the system runs from an SD or eMMC card, which
// wears out under a constantly writing machine. Unknown means false.
func OnMemoryCard() bool {
	b, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}
	return rootOnMemoryCard(string(b))
}

func rootOnMemoryCard(mounts string) bool {
	for _, line := range strings.Split(mounts, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[1] == "/" {
			return strings.HasPrefix(f[0], "/dev/mmcblk")
		}
	}
	return false
}

// Assess turns a resource reading into advice. It speaks at the first warning,
// before the Pod is slow, and again with a plain "this Pod is too small" once
// memory has been short for a long time. sustainedMemory is that long time.
func Assess(s LiveStats, onMemoryCard, sustainedMemory bool) []Advice {
	var out []Advice
	add := func(key, text string, urgent bool, cooldown time.Duration) {
		out = append(out, Advice{Key: key, Text: text, Urgent: urgent, Cooldown: cooldown})
	}

	switch s.Storage {
	case PressureCritical:
		add("storage-critical", fmt.Sprintf("I'm almost out of storage: %d GB left on the Pod. When it fills up I can't save memory or files. Delete old downloads or screenshots in Files, or move me to a bigger drive.", s.DiskFreeGB), true, 12*time.Hour)
	case PressureWarning:
		add("storage-low", fmt.Sprintf("Storage is getting low: %d GB left on the Pod (%d%%). Nothing is wrong yet. Clearing old downloads or screenshots in Files now keeps me fast, and a bigger drive means you never think about it.", s.DiskFreeGB, s.DiskFreePct), false, 3*24*time.Hour)
	}

	switch {
	case s.Memory == PressureCritical:
		add("memory-critical", fmt.Sprintf("I'm short on memory: %d MB free of %d MB. I may be slow or restart. If a browser task is running, let it finish before asking for another.", s.MemAvailableMB, s.MemTotalMB), false, 12*time.Hour)
	case s.Memory == PressureWarning:
		add("memory-low", fmt.Sprintf("Memory is getting tight: %d MB free of %d MB. Browsing is what uses it most, so I'm going to do one browser job at a time.", s.MemAvailableMB, s.MemTotalMB), false, 2*24*time.Hour)
	}
	if sustainedMemory {
		add("memory-upgrade", fmt.Sprintf("This Pod has been short on memory for a while (%d MB in total). It's too small for how you use me. A Pod with 8 GB or more would fix it for good.", s.MemTotalMB), false, 7*24*time.Hour)
	}

	if s.MemTotalMB > 0 && s.MemTotalMB < MinMemoryMB {
		add("undersized-memory", fmt.Sprintf("This Pod has %d MB of memory. I need about 4 GB to browse and stay responsive, so expect me to be slow at web tasks. 8 GB is comfortable.", s.MemTotalMB), false, 30*24*time.Hour)
	}
	if s.DiskTotalGB > 0 && s.DiskTotalGB < MinDiskGB {
		add("undersized-storage", fmt.Sprintf("This Pod's drive is %d GB. I keep memory, files and backups there, and it will fill up. 256 GB is comfortable.", s.DiskTotalGB), false, 30*24*time.Hour)
	}
	if onMemoryCard {
		add("memory-card", "I'm running from a memory card. Cards wear out and can corrupt when they're written to all day, and a power cut is when it happens. Moving me to an SSD makes me faster and much safer.", false, 30*24*time.Hour)
	}
	return out
}
