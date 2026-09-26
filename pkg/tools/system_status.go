package tools

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// SystemStatusTool reports machine health read-only: CPU temperature,
// memory, load, uptime, and disk space (size/used/free per filesystem).
//
// It exists because status questions are ordinary asks — "is the device
// healthy", "how much disk space is left" — and must be answerable on every
// surface (mobile, console, CLI, subagents) without shell, without approval,
// and without hand-scraping /proc. Free space in particular needs statvfs,
// which no plain file exposes, so a file-reading subagent could never answer
// it alone.
type SystemStatusTool struct{}

func NewSystemStatusTool() *SystemStatusTool { return &SystemStatusTool{} }

func (t *SystemStatusTool) Name() string { return "system_status" }

func (t *SystemStatusTool) Description() string {
	return "Read-only machine health in one call: CPU temperature, memory, load, uptime, and disk space (size, used, and free per filesystem). Use for 'status of the device', 'is this machine healthy', 'free disk space', 'how much space is left'."
}

func (t *SystemStatusTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *SystemStatusTool) Timeout() time.Duration { return 5 * time.Second }

func (t *SystemStatusTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	return NewToolResult(formatSystemStatus(readSystemStatus()))
}

// statusFilesystem is one mounted filesystem, sized the way df reports it.
type statusFilesystem struct {
	Device string `json:"device"`
	Mount  string `json:"mount"`
	Type   string `json:"type"`
	// TotalBytes, UsedBytes, FreeBytes: df's used/free accounting —
	// free is what an unprivileged owner can actually write (Bavail).
	TotalBytes uint64 `json:"total_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
}

// UsedPercent reports utilization like df does, or -1 for an empty fs.
func (f statusFilesystem) UsedPercent() float64 {
	denom := f.UsedBytes + f.FreeBytes
	if denom == 0 {
		return -1
	}
	return float64(f.UsedBytes) / float64(denom) * 100
}

// systemStatus is the platform-read snapshot (system_status_linux.go).
type systemStatus struct {
	CPUTempC       *float64
	MemTotalBytes  uint64
	MemAvailBytes  uint64
	SwapTotalBytes uint64
	SwapFreeBytes  uint64
	Load1          float64
	Load5          float64
	Load15         float64
	CPUCount       int
	UptimeSeconds  float64
	Filesystems    []statusFilesystem
	// Unsupported lists fields the platform could not read (honest gaps,
	// never zeros presented as measurements).
	Unsupported []string
}

// formatSystemStatus renders the snapshot as the plain data block the model
// reports from. Numbers stay exact — the owner asked for them.
func formatSystemStatus(s systemStatus) string {
	var b strings.Builder
	if s.CPUTempC != nil {
		fmt.Fprintf(&b, "CPU temperature: %.1f °C\n", *s.CPUTempC)
	} else if hasName(s.Unsupported, "cpu temperature") {
		b.WriteString("CPU temperature: not readable on this machine\n")
	}
	if s.MemTotalBytes > 0 {
		fmt.Fprintf(&b, "Memory: %s available of %s total\n",
			humanBytes(s.MemAvailBytes), humanBytes(s.MemTotalBytes))
	}
	if s.SwapTotalBytes > 0 {
		fmt.Fprintf(&b, "Swap: %s total, %s free\n",
			humanBytes(s.SwapTotalBytes), humanBytes(s.SwapFreeBytes))
	}
	if s.CPUCount > 0 {
		fmt.Fprintf(&b, "Load average: %.2f %.2f %.2f over %d CPUs\n",
			s.Load1, s.Load5, s.Load15, s.CPUCount)
	}
	if s.UptimeSeconds > 0 {
		fmt.Fprintf(&b, "Uptime: %s\n", humanDuration(time.Duration(s.UptimeSeconds*float64(time.Second))))
	}
	if len(s.Filesystems) == 0 && hasName(s.Unsupported, "disk space") {
		b.WriteString("Disk space: not readable on this machine\n")
	}
	for _, f := range s.Filesystems {
		pct := f.UsedPercent()
		pctTxt := "—"
		if pct >= 0 {
			pctTxt = fmt.Sprintf("%.0f%%", pct)
		}
		fmt.Fprintf(&b, "Disk %s (%s, %s): %s total, %s used, %s free (%s used)\n",
			f.Mount, f.Device, f.Type, humanBytes(f.TotalBytes),
			humanBytes(f.UsedBytes), humanBytes(f.FreeBytes), pctTxt)
	}
	return strings.TrimSpace(b.String())
}

func hasName(list []string, name string) bool {
	for _, n := range list {
		if n == name {
			return true
		}
	}
	return false
}

// humanBytes renders byte counts the way df -h does.
func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// humanDuration renders uptime compactly ("5m", "3h 12m", "2d 4h").
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
}
