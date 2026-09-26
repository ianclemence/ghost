package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// readSystemStatus gathers the machine snapshot from kernel-exported files
// and statvfs — all read-only, all local, no shell. Fields that cannot be
// read are reported as unsupported instead of being faked as zeros.
func readSystemStatus() systemStatus {
	var s systemStatus
	s.CPUCount = runtime.NumCPU()
	s.CPUTempC = readCPUTemp()
	if s.CPUTempC == nil {
		s.Unsupported = append(s.Unsupported, "cpu temperature")
	}
	readMemInfo(&s)
	readLoadUptime(&s)
	s.Filesystems = readFilesystems()
	if len(s.Filesystems) == 0 {
		s.Unsupported = append(s.Unsupported, "disk space")
	}
	return s
}

// readCPUTemp returns the CPU package temperature in °C from thermal zones,
// preferring a zone whose type identifies the CPU/SoC.
func readCPUTemp() *float64 {
	zones, err := filepath.Glob("/sys/class/thermal/thermal_zone*/temp")
	if err != nil || len(zones) == 0 {
		return nil
	}
	var fallback *float64
	for _, zone := range zones {
		raw, err := os.ReadFile(zone)
		if err != nil {
			continue
		}
		milli, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
		if err != nil {
			continue
		}
		c := milli / 1000
		if c < -50 || c > 150 {
			continue
		}
		zoneType := ""
		if b, err := os.ReadFile(filepath.Join(filepath.Dir(zone), "type")); err == nil {
			zoneType = strings.ToLower(strings.TrimSpace(string(b)))
		}
		if strings.Contains(zoneType, "cpu") || strings.Contains(zoneType, "soc") ||
			strings.Contains(zoneType, "x86_pkg") || strings.Contains(zoneType, "k10temp") {
			return &c
		}
		if fallback == nil {
			v := c
			fallback = &v
		}
	}
	return fallback
}

// readMemInfo parses /proc/meminfo (kB fields → bytes).
func readMemInfo(s *systemStatus) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		s.Unsupported = append(s.Unsupported, "memory")
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			s.MemTotalBytes = kb * 1024
		case "MemAvailable:":
			s.MemAvailBytes = kb * 1024
		case "SwapTotal:":
			s.SwapTotalBytes = kb * 1024
		case "SwapFree:":
			s.SwapFreeBytes = kb * 1024
		}
	}
}

// readLoadUptime parses /proc/loadavg and /proc/uptime.
func readLoadUptime(s *systemStatus) {
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 3 {
			s.Load1, _ = strconv.ParseFloat(fields[0], 64)
			s.Load5, _ = strconv.ParseFloat(fields[1], 64)
			s.Load15, _ = strconv.ParseFloat(fields[2], 64)
		}
	} else {
		s.Unsupported = append(s.Unsupported, "load average")
	}
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		if fields := strings.Fields(string(data)); len(fields) >= 1 {
			s.UptimeSeconds, _ = strconv.ParseFloat(fields[0], 64)
		}
	} else {
		s.Unsupported = append(s.Unsupported, "uptime")
	}
}

// diskFSTypes are the filesystems df reports as real storage. Pseudo
// mounts (proc, sysfs, devtmpfs, tmpfs, cgroup…) are excluded — they are
// kernel surfaces, not disk space an owner can free.
var diskFSTypes = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true,
	"vfat": true, "msdos": true, "exfat": true,
	"btrfs": true, "xfs": true, "f2fs": true,
	"ntfs": true, "ntfs3": true, "hfs": true, "hfsplus": true,
	"zfs": true, "apfs": true, "udf": true,
}

// readFilesystems walks /proc/mounts and sizes each real filesystem with
// statvfs — the exact answer df gives, without running df.
func readFilesystems() []statusFilesystem {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil
	}
	var out []statusFilesystem
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		device, mount, fstype := unescapeMount(fields[0]), unescapeMount(fields[1]), fields[2]
		if !diskFSTypes[fstype] {
			continue
		}
		var st syscall.Statfs_t
		if err := syscall.Statfs(mount, &st); err != nil {
			continue
		}
		bsize := uint64(st.Bsize)
		total := st.Blocks * bsize
		free := st.Bavail * bsize
		bFree := st.Bfree * bsize
		var used uint64
		if total >= bFree {
			used = total - bFree
		}
		out = append(out, statusFilesystem{
			Device:     device,
			Mount:      mount,
			Type:       fstype,
			TotalBytes: total,
			UsedBytes:  used,
			FreeBytes:  free,
		})
	}
	return out
}

// unescapeMount decodes the octal escapes /proc/mounts uses for spaces.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
