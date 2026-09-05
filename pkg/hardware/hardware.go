// Package hardware normalizes the machine Ghost runs on into the small
// profile the runtime actually needs — then derives appliance defaults
// automatically. Same Ghost, different hardware, appropriate defaults;
// the owner never configures model tiers, concurrency, or context sizes.
//
// Supported classes: Raspberry Pi 5, RK1-class (16GB ARM SBC),
// x86 mini-PC, GPU workstation/server, plus generic fallback. Detection
// is best-effort stdlib (/proc, runtime); unknown machines get safe
// generic defaults, never failures.
package hardware

import (
	"os"
	"runtime"
	"strings"
)

// Class is the normalized machine class.
type Class string

const (
	ClassPi5        Class = "raspberry-pi-5"
	ClassRK1        Class = "rk1-class"
	ClassMiniPC     Class = "x86-mini-pc"
	ClassGPUServer  Class = "gpu-server"
	ClassGeneric    Class = "generic"
	ClassGenericARM Class = "generic-arm"
)

// Accelerator is the available compute accelerator class.
type Accelerator string

const (
	AccelNone    Accelerator = "none"
	AccelCUDA    Accelerator = "cuda"
	AccelROCM    Accelerator = "rocm"
	AccelMetal   Accelerator = "metal"
	AccelUnknown Accelerator = "unknown"
)

// Profile is the normalized hardware view.
type Profile struct {
	Class     Class       `json:"class"`
	Arch      string      `json:"arch"`
	RAMGB     int         `json:"ram_gb"`
	Accel     Accelerator `json:"accelerator"`
	StorageGB int         `json:"storage_gb"`
	OS        string      `json:"os"`
	Cores     int         `json:"cores"`
}

// Defaults derived from the profile (opinionated, automatic).
type Defaults struct {
	ModelTier        string `json:"model_tier"` // "small" | "medium" | "large"
	MaxConcurrency   int    `json:"max_concurrency"`
	ContextTokens    int    `json:"context_tokens"`
	MemoryBudgetMB   int    `json:"memory_budget_mb"`
	LocalVoiceOK     bool   `json:"local_voice_ok"`
	PreferLocalBrain bool   `json:"prefer_local_brain"`
}

// Detect builds the profile for this machine.
func Detect() Profile {
	p := Profile{
		Arch:  runtime.GOARCH,
		OS:    runtime.GOOS,
		Cores: runtime.NumCPU(),
		RAMGB: detectRAMGB(),
		Accel: detectAccel(),
	}
	p.Class = classify(p)
	return p
}

func detectRAMGB() int {
	// Linux: /proc/meminfo. Elsewhere: conservative fallback.
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				var kb int
				if _, err := sscanfKB(line, &kb); err == nil && kb > 0 {
					return kb / 1024 / 1024
				}
			}
		}
	}
	return 4 // safe assumption for unknown machines
}

func sscanfKB(line string, kb *int) (int, error) {
	var n int
	for _, f := range strings.Fields(line) {
		var v int
		ok := true
		for _, ch := range f {
			if ch < '0' || ch > '9' {
				ok = false
				break
			}
		}
		if ok && f != "" {
			v = atoi(f)
			if v > n {
				n = v
			}
		}
	}
	*kb = n
	return 1, nil
}

func atoi(s string) int {
	n := 0
	for _, ch := range s {
		n = n*10 + int(ch-'0')
	}
	return n
}

func detectAccel() Accelerator {
	// Best-effort, no fragile vendor CLIs: presence probes only.
	if _, err := os.Stat("/dev/nvidia0"); err == nil {
		return AccelCUDA
	}
	if _, err := os.Stat("/dev/kfd"); err == nil {
		return AccelROCM
	}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return AccelMetal
	}
	if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
		return AccelNone
	}
	return AccelUnknown
}

func classify(p Profile) Class {
	if p.Accel == AccelCUDA || p.Accel == AccelROCM {
		return ClassGPUServer
	}
	if p.Arch == "arm64" {
		if p.RAMGB >= 12 {
			return ClassRK1
		}
		if p.RAMGB >= 4 && isPi() {
			return ClassPi5
		}
		return ClassGenericARM
	}
	if p.Arch == "amd64" {
		if p.RAMGB >= 32 {
			return ClassGPUServer // big x86 box; treat as server-class
		}
		return ClassMiniPC
	}
	return ClassGeneric
}

func isPi() bool {
	data, err := os.ReadFile("/proc/device-tree/model")
	if err != nil {
		data, err = os.ReadFile("/proc/cpuinfo")
	}
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "raspberry")
}

// DefaultsFor derives appliance defaults. Small models on constrained
// boards, larger where headroom exists; voice needs either an
// accelerator or a generous CPU.
func DefaultsFor(p Profile) Defaults {
	switch p.Class {
	case ClassPi5:
		return Defaults{ModelTier: "small", MaxConcurrency: 2, ContextTokens: 8192, MemoryBudgetMB: 512, LocalVoiceOK: false, PreferLocalBrain: true}
	case ClassRK1:
		return Defaults{ModelTier: "medium", MaxConcurrency: 4, ContextTokens: 16384, MemoryBudgetMB: 1024, LocalVoiceOK: true, PreferLocalBrain: true}
	case ClassMiniPC:
		return Defaults{ModelTier: "medium", MaxConcurrency: 4, ContextTokens: 32768, MemoryBudgetMB: 2048, LocalVoiceOK: true, PreferLocalBrain: true}
	case ClassGPUServer:
		return Defaults{ModelTier: "large", MaxConcurrency: 8, ContextTokens: 65536, MemoryBudgetMB: 4096, LocalVoiceOK: true, PreferLocalBrain: false}
	case ClassGenericARM:
		return Defaults{ModelTier: "small", MaxConcurrency: 2, ContextTokens: 8192, MemoryBudgetMB: 512, LocalVoiceOK: false, PreferLocalBrain: true}
	default:
		return Defaults{ModelTier: "small", MaxConcurrency: 2, ContextTokens: 8192, MemoryBudgetMB: 512, LocalVoiceOK: false, PreferLocalBrain: true}
	}
}
