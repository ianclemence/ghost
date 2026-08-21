// ghost-relay is the Ghost relay server.
//
// It accepts persistent outbound WebSocket connections from Ghost devices
// and HTTP/WebSocket connections from paired mobile apps, routing traffic
// between them.
//
// Usage:
//
//	ghost-relay serve [--listen :8080] [--registry registry.json] [--tls-cert cert.pem --tls-key key.pem]
//	ghost-relay add-device <device_id> [--name "My Ghost"]
//	ghost-relay list-devices
//	ghost-relay remove-device <device_id>
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/ianclemence/ghost/pkg/relay/server"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		cmdServe()
	case "add-device":
		cmdAddDevice()
	case "list-devices":
		cmdListDevices()
	case "remove-device":
		cmdRemoveDevice()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `ghost-relay — Ghost relay server

Usage:
  ghost-relay serve [flags]
  ghost-relay add-device <device_id> [--name "My Ghost"] [--registry registry.json]
  ghost-relay list-devices [--registry registry.json]
  ghost-relay remove-device <device_id> [--registry registry.json]`)
}

func cmdServe() {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	listen := fs.String("listen", ":8080", "listen address")
	registryPath := fs.String("registry", "registry.json", "path to device registry")
	tlsCert := fs.String("tls-cert", "", "TLS certificate file (empty = no TLS)")
	tlsKey := fs.String("tls-key", "", "TLS key file")
	adminSecret := fs.String("admin-secret", os.Getenv("GHOST_RELAY_ADMIN_SECRET"), "admin token for device enrollment")
	fs.Parse(os.Args[2:])

	cfg := server.Config{
		ListenAddr:   *listen,
		TLSCertFile:  *tlsCert,
		TLSKeyFile:   *tlsKey,
		RegistryPath: *registryPath,
		AdminSecret:  *adminSecret,
	}

	srv, err := server.NewServer(cfg)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}

	if err := srv.Serve(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func cmdAddDevice() {
	fs := flag.NewFlagSet("add-device", flag.ExitOnError)
	name := fs.String("name", "", "display name")
	registryPath := fs.String("registry", "registry.json", "path to device registry")
	fs.Parse(os.Args[2:])

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ghost-relay add-device <device_id> [--name \"My Ghost\"]")
		os.Exit(1)
	}
	deviceID := fs.Arg(0)

	reg, err := server.NewRegistry(*registryPath)
	if err != nil {
		log.Fatalf("open registry: %v", err)
	}

	secret, err := reg.Add(deviceID, *name)
	if err != nil {
		log.Fatalf("add device: %v", err)
	}

	fmt.Printf("Device registered: %s\n", deviceID)
	fmt.Printf("Secret: %s\n", secret)
	fmt.Println("\nAdd this to your Ghost device's .secrets.json as relay_device_secret.")
}

func cmdListDevices() {
	fs := flag.NewFlagSet("list-devices", flag.ExitOnError)
	registryPath := fs.String("registry", "registry.json", "path to device registry")
	fs.Parse(os.Args[2:])

	reg, err := server.NewRegistry(*registryPath)
	if err != nil {
		log.Fatalf("open registry: %v", err)
	}

	devices := reg.List()
	if len(devices) == 0 {
		fmt.Println("No devices registered.")
		return
	}

	for _, d := range devices {
		name := d.DisplayName
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Printf("  %s  %s  registered %s\n", d.DeviceID, name, d.RegisteredAt)
	}
}

func cmdRemoveDevice() {
	fs := flag.NewFlagSet("remove-device", flag.ExitOnError)
	registryPath := fs.String("registry", "registry.json", "path to device registry")
	fs.Parse(os.Args[2:])

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ghost-relay remove-device <device_id>")
		os.Exit(1)
	}
	deviceID := fs.Arg(0)

	reg, err := server.NewRegistry(*registryPath)
	if err != nil {
		log.Fatalf("open registry: %v", err)
	}

	if err := reg.Remove(deviceID); err != nil {
		log.Fatalf("remove device: %v", err)
	}
	fmt.Printf("Device removed: %s\n", deviceID)
}

// EncodeJSON is used by tests.
func EncodeJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

// ParseJSON is used by tests.
func ParseJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// Contains is used by tests.
func Contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
