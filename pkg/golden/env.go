package golden

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Env is the machine a run happened on. Results from different hardware are
// not the same test: memory and disk pressure change what an agent can finish.
type Env struct {
	Arch       string `json:"arch"`
	CPUs       int    `json:"cpus"`
	MemTotalMB int    `json:"mem_total_mb,omitempty"`
	MemFreeMB  int    `json:"mem_available_mb,omitempty"`
	DiskFreeMB int    `json:"disk_free_mb,omitempty"`
}

func CaptureEnv(dir string) Env {
	if dir == "" {
		dir = os.TempDir()
	}
	e := Env{Arch: runtime.GOARCH, CPUs: runtime.NumCPU()}
	if f, err := os.Open("/proc/meminfo"); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 2 {
				continue
			}
			kb, err := strconv.Atoi(fields[1])
			if err != nil {
				continue
			}
			switch fields[0] {
			case "MemTotal:":
				e.MemTotalMB = kb / 1024
			case "MemAvailable:":
				e.MemFreeMB = kb / 1024
			}
		}
	}
	e.DiskFreeMB = diskFreeMB(dir)
	return e
}
