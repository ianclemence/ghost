package appliance

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// MDNSAdvertiser handles mDNS/Bonjour service advertisement.
// This allows Ghost to be discovered on the local network as "ghost.local".
type MDNSAdvertiser struct {
	Hostname string
	Port     int
	Version  string
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
	// Check if we're on Linux
	if os.Getenv("GHOST_DIR") == "" {
		// Not in appliance mode, skip
		return fmt.Errorf("not in appliance mode")
	}

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

	// Build TXT records
	txtRecords := fmt.Sprintf("version=%s api_port=%d", m.Version, m.Port)

	// avahi-publish -R -s <name> <type> <port> [key=value ...]
	args := []string{
		"-R", // Register
		"-s", // Service
		m.Hostname,
		serviceType,
		fmt.Sprintf("%d", m.Port),
		txtRecords,
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
