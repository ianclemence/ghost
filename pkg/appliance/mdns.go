package appliance

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// MDNSAdvertiser handles mDNS/Bonjour service advertisement.
// This allows Ghost to be discovered on the local network as "ghost.local".
type MDNSAdvertiser struct {
	Hostname string
	Port     int
	Version  string
	// TXT carries additional key=value discovery records (pod_id, transport,
	// console_port, api_port, setup). Entries with empty keys or values are
	// omitted. Keep values short and free of spaces.
	TXT map[string]string
}

// NewMDNSAdvertiser creates a new mDNS advertiser.
func NewMDNSAdvertiser(port int, version string) *MDNSAdvertiser {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "ghost"
	}

	return &MDNSAdvertiser{
		Hostname: hostname,
		Port:     port,
		Version:  version,
	}
}

// Available reports whether advertisement can run here: avahi-publish must
// exist. No environment gating — presence of the daemon tooling is the only
// precondition, so dev checkouts and devices behave the same.
func (m *MDNSAdvertiser) Available() error {
	return m.checkAvahi()
}

// Advertise registers the Ghost service via mDNS.
// On Linux, this uses avahi-daemon. On other platforms, it's a no-op.
func (m *MDNSAdvertiser) Advertise() error {
	// Check if avahi-publish is available (Linux)
	if err := m.checkAvahi(); err != nil {
		log.Printf("mDNS: avahi not available, skipping advertisement: %v", err)
		return nil
	}

	// Register via avahi-publish
	go m.registerAvahi()

	// Also try to resolve ghost.local to verify it works
	go m.verifyResolution()

	return nil
}

// Stop unregisters the mDNS service.
func (m *MDNSAdvertiser) Stop() {
	// avahi-publish runs in background, will stop when process exits
}

func (m *MDNSAdvertiser) checkAvahi() error {
	// Check if avahi-publish exists
	_, err := os.Stat("/usr/bin/avahi-publish")
	if err != nil {
		_, err = os.Stat("/usr/sbin/avahi-publish")
		if err != nil {
			return fmt.Errorf("avahi-publish not found")
		}
	}
	return nil
}

func (m *MDNSAdvertiser) registerAvahi() {
	// Build service type
	serviceType := "_ghost._tcp"

	// avahi-publish expects each TXT record as its own key=value argument.
	// Passing one space-joined string (the previous behaviour) makes the
	// daemon treat it as a single opaque record that no client can parse.
	txt := map[string]string{
		"version":  m.Version,
		"api_port": fmt.Sprintf("%d", m.Port),
	}
	for k, v := range m.TXT {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		txt[k] = v
	}
	keys := make([]string, 0, len(txt))
	for k := range txt {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// avahi-publish -R -s <name> <type> <port> [key=value ...]
	args := []string{
		"-R", // Register
		"-s", // Service
		m.Hostname,
		serviceType,
		fmt.Sprintf("%d", m.Port),
	}
	for _, k := range keys {
		args = append(args, k+"="+txt[k])
	}

	// Try both paths
	paths := []string{"/usr/bin/avahi-publish", "/usr/sbin/avahi-publish"}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			log.Printf("mDNS: advertising as %s.local:%d", m.Hostname, m.Port)
			// Run in background, will keep running
			cmd := exec.Command(path, args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				log.Printf("mDNS: failed to start avahi-publish: %v", err)
			}
			return
		}
	}
}

func (m *MDNSAdvertiser) verifyResolution() {
	// Wait a moment for mDNS to register
	time.Sleep(2 * time.Second)

	// Try to resolve ghost.local
	addrs, err := net.LookupHost("ghost.local")
	if err != nil {
		log.Printf("mDNS: ghost.local resolution failed (expected if avahi not running): %v", err)
		return
	}

	if len(addrs) > 0 {
		log.Printf("mDNS: ghost.local resolves to %s", strings.Join(addrs, ", "))
	}
}

// GetLocalIP returns the primary local IP address.
func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "unknown"
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "unknown"
}
