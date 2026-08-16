package tools

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// NetworkingTool provides information on Tailscale, Bonjour, and other networking features.
// Adapted from OpenClaw's networking/tailscale features.
type NetworkingTool struct {
	workspace string
}

func NewNetworkingTool(workspace string) *NetworkingTool {
	return &NetworkingTool{
		workspace: workspace,
	}
}

func (t *NetworkingTool) Name() string {
	return "networking"
}

func (t *NetworkingTool) Description() string {
	return "Check networking status, Tailscale IP, Bonjour services, or discover other Ghost agents on the local network."
}

func (t *NetworkingTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"status", "tailscale", "bonjour"},
				"description": "The networking action to perform (default: 'status').",
			},
		},
	}
}

func (t *NetworkingTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	action, ok := args["action"].(string)
	if !ok || action == "" {
		action = "status"
	}

	switch action {
	case "status":
		return t.getStatus()
	case "tailscale":
		return t.getTailscaleInfo()
	case "bonjour":
		return t.getBonjourInfo()
	default:
		return ErrorResult(fmt.Sprintf("Unsupported networking action: %s", action))
	}
}

func (t *NetworkingTool) getStatus() *ToolResult {
	var sb strings.Builder
	sb.WriteString("# Networking Status\n\n")

	// Local IP addresses
	ifaces, err := net.Interfaces()
	if err == nil {
		sb.WriteString("## Local Interfaces\n")
		for _, i := range ifaces {
			addrs, err := i.Addrs()
			if err == nil {
				for _, addr := range addrs {
					if ip, ok := addr.(*net.IPNet); ok && !ip.IP.IsLoopback() {
						sb.WriteString(fmt.Sprintf("- %s: %s\n", i.Name, addr.String()))
					}
				}
			}
		}
		sb.WriteString("\n")
	}

	// Check Tailscale
	if _, err := exec.LookPath("tailscale"); err == nil {
		sb.WriteString("## Tailscale (Installed)\n")
		cmd := exec.Command("tailscale", "ip")
		if out, err := cmd.CombinedOutput(); err == nil {
			sb.WriteString(fmt.Sprintf("- IP: %s\n", strings.TrimSpace(string(out))))
		} else {
			sb.WriteString("- Status: Offline or Not Logged In\n")
		}
		sb.WriteString("\n")
	}

	return NewToolResult(sb.String())
}

func (t *NetworkingTool) getTailscaleInfo() *ToolResult {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return ErrorResult("Tailscale is not installed on this system.")
	}
	
	cmd := exec.Command("tailscale", "status")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to get tailscale status: %v\nOutput: %s", err, string(out)))
	}
	
	return NewToolResult(fmt.Sprintf("# Tailscale Status\n\n```\n%s\n```", string(out)))
}

func (t *NetworkingTool) getBonjourInfo() *ToolResult {
	// avahi-browse is the standard tool for Bonjour on Linux (Avahi)
	if _, err := exec.LookPath("avahi-browse"); err != nil {
		return ErrorResult("Avahi (avahi-browse) is not installed. Please install 'avahi-utils' on your Pi.")
	}

	cmd := exec.Command("avahi-browse", "-t", "-a", "-r")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to browse Bonjour services: %v\nOutput: %s", err, string(out)))
	}
	
	return NewToolResult(fmt.Sprintf("# Bonjour Services (Local Network Discovery)\n\n```\n%s\n```", string(out)))
}
