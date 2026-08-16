package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/appliance"
	"github.com/ianclemence/ghost/pkg/config"
)

//go:embed web/*
var webFiles embed.FS

var (
	version = "dev"
	fb      *appliance.FirstBoot
)

func main() {
	port := flag.Int("port", 80, "HTTP port for setup wizard")
	ghostDir := flag.String("dir", "/var/ghost", "Ghost installation directory")
	flag.Parse()

	fb = appliance.NewFirstBoot()
	fb.GhostDir = *ghostDir
	fb.ConfigDir = filepath.Join(*ghostDir, "config")
	fb.DataDir = filepath.Join(*ghostDir, "data")
	fb.Workspace = filepath.Join(*ghostDir, "workspace")
	fb.ConfigPath = filepath.Join(*ghostDir, "config", "config.json")
	fb.EnvPath = filepath.Join(*ghostDir, ".env")

	// Check if first boot is needed
	if !fb.IsFirstBoot() {
		log.Println("Setup already complete. Use --force to re-run.")
		os.Exit(0)
	}

	log.Printf("Ghost First Boot Wizard v%s", version)
	log.Printf("Starting setup wizard on port %d...", *port)

	// Ensure directories exist
	if err := fb.EnsureDirectories(); err != nil {
		log.Fatalf("Failed to create directories: %v", err)
	}

	// Start the wizard server
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleWizardIndex)
	mux.HandleFunc("/api/scan-wifi", handleScanWiFi)
	mux.HandleFunc("/api/connect-wifi", handleConnectWiFi)
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/configure", handleConfigure)
	mux.HandleFunc("/api/pairing-code", handlePairingCode)
	mux.HandleFunc("/api/ollama/models", handleOllamaModels)
	mux.HandleFunc("/api/ollama/pull", handleOllamaPull)

	addr := fmt.Sprintf("0.0.0.0:%d", *port)
	log.Printf("Setup wizard running at http://ghost.local:%d", *port)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func handleWizardIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data, err := webFiles.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "Failed to load wizard", http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func handleScanWiFi(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Scan for WiFi networks
	networks, err := scanWiFiNetworks()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      false,
			"error":   err.Error(),
			"networks": []WiFiNetwork{},
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"networks": networks,
	})
}

func handleConnectWiFi(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SSID     string `json:"ssid"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"ok":false,"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := connectToWiFi(req.SSID, req.Password); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true,
	})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"first_boot": fb.IsFirstBoot(),
		"version":    version,
	})
}

func handleConfigure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		AdminPassword string `json:"admin_password"`
		Model         string `json:"model"`
		Provider      string `json:"provider"`
		OllamaURL     string `json:"ollama_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"ok":false,"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Generate config
	if err := generateConfig(req.AdminPassword, req.Model, req.Provider, req.OllamaURL); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	// Mark setup complete
	if err := fb.MarkSetupComplete(); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "failed to mark setup complete: " + err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    true,
		"message": "Setup complete! Ghost will restart shortly.",
	})
}

func handlePairingCode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	code, err := appliance.GeneratePairingCode()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":   true,
		"code": code,
	})
}

func handleOllamaModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	models, err := listOllamaModels()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":     false,
			"error":  err.Error(),
			"models": []string{},
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"models": models,
	})
}

func handleOllamaPull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"ok":false,"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Pull model in background
	go pullOllamaModel(req.Model)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"message": "Download started",
	})
}

// generateConfig creates the .env and config.json files.
func generateConfig(adminPassword, model, provider, ollamaURL string) error {
	// Generate .env
	envContent := fmt.Sprintf(`# Ghost Appliance Configuration
# Generated by setup wizard

# API Authentication
BRIDGE_SECRET=%s

# API Server
GHOST_API_PORT=8766

# Data Paths
GHOST_DB_PATH=%s/ghost.db
MEMORY_DIR=%s/memory

# Timezone
TZ=UTC
`, adminPassword, fb.Workspace, fb.Workspace)

	if err := os.WriteFile(fb.EnvPath, []byte(envContent), 0600); err != nil {
		return fmt.Errorf("failed to write .env: %w", err)
	}

	// Load default config and customize
	cfg := config.DefaultConfig()

	// Set model configuration
	if model != "" {
		cfg.Agents.Defaults.Model = model
	}
	if provider != "" {
		cfg.Agents.Defaults.Provider = provider
	}

	// Set workspace
	cfg.Agents.Defaults.Workspace = fb.Workspace

	// Configure Ollama
	if ollamaURL != "" {
		cfg.Providers.Ollama.APIBase = ollamaURL
	} else {
		cfg.Providers.Ollama.APIBase = "http://localhost:11434"
	}

	// Set gateway secret
	cfg.Gateway.BridgeSecret = adminPassword
	cfg.Gateway.Port = 8766

	// Enable heartbeat
	cfg.Heartbeat.Enabled = true
	cfg.Heartbeat.Interval = 30

	// Enable RAG
	cfg.RAG.Enabled = true

	// Save config
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(fb.ConfigPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config.json: %w", err)
	}

	return nil
}

// WiFiNetwork represents a scanned WiFi network.
type WiFiNetwork struct {
	SSID       string `json:"ssid"`
	Signal     int    `json:"signal"`
	Encrypted  bool   `json:"encrypted"`
}

// scanWiFiNetworks scans for available WiFi networks.
func scanWiFiNetworks() ([]WiFiNetwork, error) {
	// Try iwlist scan
	out, err := exec.Command("iwlist", "wlan0", "scan").CombinedOutput()
	if err != nil {
		// Try nmcli as fallback
		return scanWiFiNmcli()
	}

	return parseIwlistOutput(string(out)), nil
}

func scanWiFiNmcli() ([]WiFiNetwork, error) {
	out, err := exec.Command("nmcli", "-t", "-f", "SSID,SIGNAL,SECURITY", "dev", "wifi", "list").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to scan WiFi: %w", err)
	}

	var networks []WiFiNetwork
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 2 {
			continue
		}
		ssid := parts[0]
		if ssid == "" {
			continue
		}
		signal := 0
		fmt.Sscanf(parts[1], "%d", &signal)
		encrypted := len(parts) > 2 && parts[2] != ""
		networks = append(networks, WiFiNetwork{
			SSID:      ssid,
			Signal:    signal,
			Encrypted: encrypted,
		})
	}
	return networks, nil
}

func parseIwlistOutput(output string) []WiFiNetwork {
	var networks []WiFiNetwork
	seen := make(map[string]bool)

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "ESSID:") {
			ssid := strings.TrimPrefix(line, "ESSID:\"")
			ssid = strings.TrimSuffix(ssid, "\"")
			if ssid != "" && !seen[ssid] {
				seen[ssid] = true
				networks = append(networks, WiFiNetwork{
					SSID:      ssid,
					Signal:    70, // Default signal strength
					Encrypted: true,
				})
			}
		}
	}
	return networks
}

// connectToWiFi connects to a WiFi network.
func connectToWiFi(ssid, password string) error {
	// Try nmcli first
	if password == "" {
		// Open network
		out, err := exec.Command("nmcli", "dev", "wifi", "connect", ssid).CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to connect: %s", string(out))
		}
	} else {
		out, err := exec.Command("nmcli", "dev", "wifi", "connect", ssid, "password", password).CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to connect: %s", string(out))
		}
	}
	return nil
}

// listOllamaModels lists locally installed Ollama models.
func listOllamaModels() ([]string, error) {
	out, err := exec.Command("ollama", "list").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}

	var models []string
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if i == 0 {
			continue // Skip header
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			models = append(models, parts[0])
		}
	}
	return models, nil
}

// pullOllamaModel downloads an Ollama model.
func pullOllamaModel(model string) error {
	cmd := exec.Command("ollama", "pull", model)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
