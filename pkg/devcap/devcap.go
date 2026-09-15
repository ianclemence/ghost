// Package devcap evaluates whether a device can realistically run a model.
// Compatibility is determined by the runtime/model combination, never by a
// naive "RAM >= X" check.
package devcap

// Device describes the hardware Ghost runs on.
type Device struct {
	Platform     string `json:"platform"` // android | ios | linux | darwin
	OSVersion    string `json:"os_version,omitempty"`
	Arch         string `json:"arch"` // arm64 | x86_64 ...
	TotalRAMMB   int64  `json:"total_ram_mb"`
	FreeDiskMB   int64  `json:"free_disk_mb"`
	Accelerator  string `json:"accelerator,omitempty"` // metal | nnapi | none
	Runtime      string `json:"runtime"`               // mobile-local | ollama | cloud
	RuntimeVers  string `json:"runtime_version,omitempty"`
	ThermalState string `json:"thermal_state,omitempty"` // nominal | fair | serious | critical
	LowMemory    bool   `json:"low_memory,omitempty"`
}

// ModelNeed describes what one model artifact requires.
type ModelNeed struct {
	ModelID     string   `json:"model_id"`
	Runtime     string   `json:"runtime"`
	Platforms   []string `json:"platforms"`
	Archs       []string `json:"archs"`
	MinRAMMB    int64    `json:"min_ram_mb"`
	RecRAMMB    int64    `json:"rec_ram_mb"`
	SizeMB      int64    `json:"size_mb"`
	MinOS       string   `json:"min_os,omitempty"`
	Accelerated bool     `json:"accelerated,omitempty"`
}

// Verdict distinguishes compatible / recommended / unavailable states.
type Verdict string

const (
	Compatible               Verdict = "compatible"
	CompatibleNotRecommended Verdict = "compatible_not_recommended"
	Incompatible             Verdict = "incompatible"
	TemporarilyUnavailable   Verdict = "temporarily_unavailable"
)

// Result is the evaluation for one model on one device.
type Result struct {
	Verdict Verdict `json:"verdict"`
	Reason  string  `json:"reason,omitempty"`
}

// Evaluate determines what a device can realistically run.
func Evaluate(d Device, m ModelNeed) Result {
	if !containsFold(m.Platforms, d.Platform) {
		return Result{Verdict: Incompatible, Reason: "platform not supported by model artifact"}
	}
	if len(m.Archs) > 0 && !containsFold(m.Archs, d.Arch) {
		return Result{Verdict: Incompatible, Reason: "architecture not supported"}
	}
	if m.Runtime != "" && d.Runtime != "" && !equalFold(m.Runtime, d.Runtime) {
		return Result{Verdict: Incompatible, Reason: "runtime mismatch"}
	}
	if m.MinOS != "" && d.OSVersion != "" && compareVersions(d.OSVersion, m.MinOS) < 0 {
		return Result{Verdict: Incompatible, Reason: "os version below minimum"}
	}
	if m.MinRAMMB > 0 && d.TotalRAMMB > 0 && d.TotalRAMMB < m.MinRAMMB {
		return Result{Verdict: Incompatible, Reason: "insufficient RAM"}
	}
	if m.SizeMB > 0 && d.FreeDiskMB > 0 && d.FreeDiskMB < m.SizeMB+512 {
		return Result{Verdict: Incompatible, Reason: "insufficient storage"}
	}
	// Transient states never report incompatible: they may clear.
	if d.LowMemory {
		return Result{Verdict: TemporarilyUnavailable, Reason: "device under memory pressure"}
	}
	switch d.ThermalState {
	case "serious", "critical":
		return Result{Verdict: TemporarilyUnavailable, Reason: "thermal throttling"}
	}
	if m.RecRAMMB > 0 && d.TotalRAMMB > 0 && d.TotalRAMMB < m.RecRAMMB {
		return Result{Verdict: CompatibleNotRecommended, Reason: "below recommended RAM; expect slow inference"}
	}
	if m.Accelerated && d.Accelerator == "" {
		return Result{Verdict: CompatibleNotRecommended, Reason: "no accelerator; CPU fallback will be slow"}
	}
	return Result{Verdict: Compatible}
}

func containsFold(list []string, v string) bool {
	for _, s := range list {
		if equalFold(s, v) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		// still compare case-insensitively for ascii
	}
	aa := lowerASCII(a)
	bb := lowerASCII(b)
	return aa == bb
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// compareVersions compares dotted versions; returns -1/0/1.
func compareVersions(a, b string) int {
	pa := splitDots(a)
	pb := splitDots(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}

func splitDots(s string) []int {
	var out []int
	cur := 0
	has := false
	for i := 0; i <= len(s); i++ {
		var c byte
		if i < len(s) {
			c = s[i]
		} else {
			c = '.'
		}
		if c >= '0' && c <= '9' {
			cur = cur*10 + int(c-'0')
			has = true
			continue
		}
		if c == '.' {
			if has {
				out = append(out, cur)
				cur = 0
				has = false
			}
			continue
		}
		break
	}
	return out
}
