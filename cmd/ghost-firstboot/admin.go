package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ianclemence/ghost/pkg/appliance"
	"github.com/ianclemence/ghost/pkg/config"
)

// updateState tracks an in-flight "ghost update" run so the UI can poll it.
type updateState struct {
	mu      sync.Mutex
	running bool
	success bool
	log     string
}

var currentUpdate updateState

// requireSession aborts the request with 401 unless a valid admin session cookie is present.
func requireSession(w http.ResponseWriter, r *http.Request) bool {
	if !sessions.valid(sessionToken(r)) {
		http.Error(w, `{"ok":false,"error":"session expired, please log in"}`, http.StatusUnauthorized)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// ---------- Phase 1: Health & Updates ----------

type serviceStatus struct {
	Name    string `json:"name"`
	Active  bool   `json:"active"`
	Enabled bool   `json:"enabled"`
}

func systemdActive(unit string) (active, enabled bool) {
	out, err := exec.Command("systemctl", "is-active", unit).CombinedOutput()
	active = err == nil && strings.TrimSpace(string(out)) == "active"
	out, err = exec.Command("systemctl", "is-enabled", unit).CombinedOutput()
	enabled = err == nil && strings.TrimSpace(string(out)) == "enabled"
	return
}

func cpuUsagePercent() float64 {
	readCPU := func() (idle, total uint64) {
		b, err := os.ReadFile("/proc/stat")
		if err != nil {
			return 0, 0
		}
		fields := strings.Fields(strings.Split(string(b), "\n")[0])
		if len(fields) < 5 || fields[0] != "cpu" {
			return 0, 0
		}
		for _, f := range fields[1:] {
			var v uint64
			fmt.Sscanf(f, "%d", &v)
			total += v
		}
		var idleU uint64
		fmt.Sscanf(fields[4], "%d", &idleU)
		return idleU, total
	}
	idle1, total1 := readCPU()
	time.Sleep(500 * time.Millisecond)
	idle2, total2 := readCPU()
	dTotal := total2 - total1
	if dTotal == 0 {
		return 0
	}
	return float64(dTotal-(idle2-idle1)) / float64(dTotal) * 100
}

func memoryInfo() (usedBytes, totalBytes uint64) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		var v uint64
		fmt.Sscanf(fields[1], "%d", &v)
		v *= 1024
		switch fields[0] {
		case "MemTotal:":
			totalBytes = v
		case "MemAvailable:":
			usedBytes = totalBytes - v
		}
	}
	return usedBytes, totalBytes
}

func diskUsage(path string) (used, total uint64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	total = st.Blocks * uint64(st.Bsize)
	free := st.Bavail * uint64(st.Bsize)
	used = total - free
	return used, total
}

func loadAverages() (one, five, fifteen float64) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	parts := strings.Fields(string(b))
	if len(parts) >= 3 {
		fmt.Sscanf(parts[0], "%f", &one)
		fmt.Sscanf(parts[1], "%f", &five)
		fmt.Sscanf(parts[2], "%f", &fifteen)
	}
	return
}

func handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	hostname, _ := os.Hostname()

	services := []serviceStatus{}
	for _, name := range []string{"ghost", "ollama", "ghost-firstboot"} {
		active, enabled := systemdActive(name)
		services = append(services, serviceStatus{Name: name, Active: active, Enabled: enabled})
	}

	usedMem, totalMem := memoryInfo()
	usedDisk, totalDisk := diskUsage(fb.GhostDir)
	one, five, fifteen := loadAverages()

	cfg, err := config.LoadConfig(fb.ConfigPath)
	model := ""
	provider := ""
	ollamaURL := ""
	ip := appliance.GetLocalIP()
	if err == nil {
		model = cfg.Agents.Defaults.Model
		provider = cfg.Agents.Defaults.Provider
		ollamaURL = cfg.Providers.Ollama.APIBase
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":         true,
		"hostname":   hostname,
		"version":    version,
		"uptime":     systemUptime(),
		"ip":         ip,
		"model":      model,
		"provider":   provider,
		"ollama_url": ollamaURL,
		"cpu_percent": cpuUsagePercent(),
		"load": map[string]float64{
			"one": one, "five": five, "fifteen": fifteen,
		},
		"memory": map[string]uint64{
			"used": usedMem, "total": totalMem,
		},
		"disk": map[string]uint64{
			"used": usedDisk, "total": totalDisk,
		},
		"services": services,
	})
}

func handleDoctor(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}

	checks := []map[string]interface{}{}
	add := func(name, status, message string) {
		checks = append(checks, map[string]interface{}{
			"name": name, "status": status, "message": message,
		})
	}

	for _, unit := range []string{"ghost", "ollama"} {
		active, _ := systemdActive(unit)
		if active {
			add(unit+" service", "ok", "running")
		} else {
			add(unit+" service", "fail", "not running")
		}
	}

	if _, err := os.Stat(fb.ConfigPath); err == nil {
		add("config", "ok", fb.ConfigPath)
	} else {
		add("config", "fail", "config.json missing")
	}

	cfg, err := config.LoadConfig(fb.ConfigPath)
	if err == nil && cfg.Gateway.BridgeSecret != "" {
		add("bridge secret", "ok", "configured")
	} else {
		add("bridge secret", "fail", "missing")
	}

	usedDisk, totalDisk := diskUsage(fb.GhostDir)
	pct := float64(0)
	if totalDisk > 0 {
		pct = float64(usedDisk) / float64(totalDisk) * 100
	}
	if pct < 90 {
		add("disk space", "ok", fmt.Sprintf("%.1f%% used", pct))
	} else {
		add("disk space", "fail", fmt.Sprintf("%.1f%% used - consider freeing space", pct))
	}

	// Ghost API reachability
	if cfg != nil && cfg.Gateway.Port > 0 {
		apiURL := fmt.Sprintf("http://127.0.0.1:%d/", cfg.Gateway.Port)
		client := &http.Client{Timeout: 5 * time.Second}
		if resp, err := client.Get(apiURL); err == nil {
			resp.Body.Close()
			add("ghost api", "ok", apiURL)
		} else {
			add("ghost api", "warn", "not responding yet")
		}
	}

	// Ollama reachability
	base := "http://localhost:11434"
	if cfg != nil && cfg.Providers.Ollama.APIBase != "" {
		base = cfg.Providers.Ollama.APIBase
	}
	client := &http.Client{Timeout: 5 * time.Second}
	if resp, err := client.Get(base + "/api/tags"); err == nil {
		resp.Body.Close()
		add("ollama api", "ok", base)
	} else {
		add("ollama api", "fail", base+" unreachable")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "checks": checks})
}

// runUpdate executes "ghost update" in the background, capturing output for polling.
func runUpdate() {
	currentUpdate.mu.Lock()
	if currentUpdate.running {
		currentUpdate.mu.Unlock()
		return
	}
	currentUpdate.running = true
	currentUpdate.success = false
	currentUpdate.log = "Starting update...\n"
	currentUpdate.mu.Unlock()

	cmd := exec.Command("ghost", "update")
	out, err := cmd.CombinedOutput()

	currentUpdate.mu.Lock()
	defer currentUpdate.mu.Unlock()
	currentUpdate.running = false
	currentUpdate.log += string(out)
	if err != nil {
		currentUpdate.success = false
		currentUpdate.log += fmt.Sprintf("\nUpdate failed: %v\n", err)
	} else {
		currentUpdate.success = true
		currentUpdate.log += "\nUpdate complete!\n"
	}
}

func handleUpdateStart(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	go runUpdate()
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Update started"})
}

func handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	currentUpdate.mu.Lock()
	defer currentUpdate.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"running": currentUpdate.running,
		"success": currentUpdate.success,
		"log":     currentUpdate.log,
	})
}

// ---------- Phase 2: AI configuration ----------

// updateEnvFile sets or replaces a KEY=VALUE line in the given .env file,
// preserving all other lines and file permissions.
func updateEnvFile(key, value string) error {
	lines := []string{}
	if b, err := os.ReadFile(fb.EnvPath); err == nil {
		lines = strings.Split(string(b), "\n")
	}

	prefix := key + "="
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			lines[i] = prefix + value
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, prefix+value)
	}

	fi, err := os.Stat(fb.EnvPath)
	perm := os.FileMode(0600)
	if err == nil {
		perm = fi.Mode().Perm()
	}
	return os.WriteFile(fb.EnvPath, []byte(strings.Join(lines, "\n")+"\n"), perm)
}

// maskKey returns the key masked except for the last 4 characters, or "" for empty keys.
func maskKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "••••••••"
	}
	return "••••••••" + key[len(key)-4:]
}

func handleConfigGet(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	cfg, err := config.LoadConfig(fb.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	providers := map[string]map[string]interface{}{
		"moonshot":  {"api_key": maskKey(cfg.Providers.Moonshot.APIKey), "api_base": cfg.Providers.Moonshot.APIBase},
		"anthropic": {"api_key": maskKey(cfg.Providers.Anthropic.APIKey), "api_base": cfg.Providers.Anthropic.APIBase},
		"openai":    {"api_key": maskKey(cfg.Providers.OpenAI.APIKey), "api_base": cfg.Providers.OpenAI.APIBase},
		"openrouter": {"api_key": maskKey(cfg.Providers.OpenRouter.APIKey), "api_base": cfg.Providers.OpenRouter.APIBase},
		"groq":      {"api_key": maskKey(cfg.Providers.Groq.APIKey), "api_base": cfg.Providers.Groq.APIBase},
		"deepseek":  {"api_key": maskKey(cfg.Providers.DeepSeek.APIKey), "api_base": cfg.Providers.DeepSeek.APIBase},
		"gemini":    {"api_key": maskKey(cfg.Providers.Gemini.APIKey), "api_base": cfg.Providers.Gemini.APIBase},
		"zhipu":     {"api_key": maskKey(cfg.Providers.Zhipu.APIKey), "api_base": cfg.Providers.Zhipu.APIBase},
		"ollama":    {"api_key": maskKey(cfg.Providers.Ollama.APIKey), "api_base": cfg.Providers.Ollama.APIBase},
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":            true,
		"provider":      cfg.Agents.Defaults.Provider,
		"model":         cfg.Agents.Defaults.Model,
		"fallback_models": cfg.Agents.Defaults.FallbackModels,
		"embedding_model": cfg.Agents.Defaults.EmbeddingModel,
		"max_tokens":    cfg.Agents.Defaults.MaxTokens,
		"temperature":   cfg.Agents.Defaults.Temperature,
		"providers":     providers,
	})
}

func handleConfigSet(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Provider        string   `json:"provider"`
		Model           string   `json:"model"`
		FallbackModels  []string `json:"fallback_models"`
		EmbeddingModel  string   `json:"embedding_model"`
		OllamaURL       string   `json:"ollama_url"`
		APIKeys         map[string]string `json:"api_keys"`
		MaxTokens       int      `json:"max_tokens"`
		Temperature     float64  `json:"temperature"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid request"})
		return
	}

	cfg, err := config.LoadConfig(fb.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	// API keys first: write to .env so LoadConfig's env overrides pick them up,
	// and to config.json for persistence.
	envKeys := map[string]string{
		"moonshot":  "KIMI_API_KEY",
		"anthropic": "ANTHROPIC_API_KEY",
		"openai":    "OPENAI_API_KEY",
		"openrouter": "OPENROUTER_API_KEY",
		"groq":      "GROQ_API_KEY",
		"deepseek":  "DEEPSEEK_API_KEY",
		"gemini":    "GEMINI_API_KEY",
		"zhipu":     "ZHIPU_API_KEY",
	}
	cfgKeys := map[string]*config.ProviderConfig{
		"moonshot":  &cfg.Providers.Moonshot,
		"anthropic": &cfg.Providers.Anthropic,
		"openai":    &cfg.Providers.OpenAI,
		"openrouter": &cfg.Providers.OpenRouter,
		"groq":      &cfg.Providers.Groq,
		"deepseek":  &cfg.Providers.DeepSeek,
		"gemini":    &cfg.Providers.Gemini,
		"zhipu":     &cfg.Providers.Zhipu,
	}
	for name, key := range req.APIKeys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		if env, ok := envKeys[name]; ok {
			if err := updateEnvFile(env, trimmed); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "failed to save API key: " + err.Error()})
				return
			}
		}
		if pc, ok := cfgKeys[name]; ok {
			pc.APIKey = trimmed
		}
	}

	if req.Provider != "" {
		cfg.Agents.Defaults.Provider = req.Provider
	}
	if req.Model != "" {
		cfg.Agents.Defaults.Model = req.Model
	}
	if req.FallbackModels != nil {
		cfg.Agents.Defaults.FallbackModels = req.FallbackModels
	}
	if req.EmbeddingModel != "" {
		cfg.Agents.Defaults.EmbeddingModel = req.EmbeddingModel
	}
	if req.OllamaURL != "" {
		cfg.Providers.Ollama.APIBase = req.OllamaURL
	}
	if req.MaxTokens > 0 {
		cfg.Agents.Defaults.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		cfg.Agents.Defaults.Temperature = req.Temperature
	}

	if err := config.SaveConfig(fb.ConfigPath, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "AI configuration saved"})
}

func handleOllamaDelete(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "model is required"})
		return
	}
	out, err := exec.Command("ollama", "rm", req.Model).CombinedOutput()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": strings.TrimSpace(string(out))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Model deleted"})
}

// ---------- Phase 3: Channels & notifications ----------

func handleChannelsGet(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	cfg, err := config.LoadConfig(fb.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true,
		"channels": map[string]interface{}{
			"telegram": map[string]interface{}{
				"enabled": cfg.Channels.Telegram.Enabled,
				"token":   maskKey(cfg.Channels.Telegram.Token),
			},
			"discord": map[string]interface{}{
				"enabled": cfg.Channels.Discord.Enabled,
				"token":   maskKey(cfg.Channels.Discord.Token),
			},
			"slack": map[string]interface{}{
				"enabled":  cfg.Channels.Slack.Enabled,
				"bot_token": maskKey(cfg.Channels.Slack.BotToken),
				"app_token": maskKey(cfg.Channels.Slack.AppToken),
			},
			"email": map[string]interface{}{
				"enabled":  cfg.Channels.Email.Enabled,
				"smtp_host": cfg.Channels.Email.SMTPHost,
				"smtp_port": cfg.Channels.Email.SMTPPort,
				"username":  cfg.Channels.Email.Username,
				"from":      cfg.Channels.Email.From,
				"to":        cfg.Channels.Email.To,
			},
			"whatsapp": map[string]interface{}{
				"enabled": cfg.Channels.WhatsApp.Enabled,
			},
		},
		"heartbeat": map[string]interface{}{
			"enabled":  cfg.Heartbeat.Enabled,
			"interval": cfg.Heartbeat.Interval,
		},
	})
}

func handleChannelsSet(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Telegram *struct {
			Enabled bool   `json:"enabled"`
			Token   string `json:"token"`
		} `json:"telegram"`
		Discord *struct {
			Enabled bool   `json:"enabled"`
			Token   string `json:"token"`
		} `json:"discord"`
		Email *struct {
			Enabled  bool   `json:"enabled"`
			SMTPHost string `json:"smtp_host"`
			SMTPPort int    `json:"smtp_port"`
			Username string `json:"username"`
			Password string `json:"password"`
			From     string `json:"from"`
			To       string `json:"to"`
		} `json:"email"`
		Heartbeat *struct {
			Enabled  bool `json:"enabled"`
			Interval int  `json:"interval"`
		} `json:"heartbeat"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid request"})
		return
	}

	cfg, err := config.LoadConfig(fb.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	if req.Telegram != nil {
		cfg.Channels.Telegram.Enabled = req.Telegram.Enabled
		if req.Telegram.Token != "" && !strings.HasPrefix(req.Telegram.Token, "••") {
			cfg.Channels.Telegram.Token = req.Telegram.Token
			updateEnvFile("TELEGRAM_BOT_TOKEN", req.Telegram.Token)
		}
	}
	if req.Discord != nil {
		cfg.Channels.Discord.Enabled = req.Discord.Enabled
		if req.Discord.Token != "" && !strings.HasPrefix(req.Discord.Token, "••") {
			cfg.Channels.Discord.Token = req.Discord.Token
			updateEnvFile("DISCORD_BOT_TOKEN", req.Discord.Token)
		}
	}
	if req.Email != nil {
		cfg.Channels.Email.Enabled = req.Email.Enabled
		if req.Email.SMTPHost != "" {
			cfg.Channels.Email.SMTPHost = req.Email.SMTPHost
		}
		if req.Email.SMTPPort != 0 {
			cfg.Channels.Email.SMTPPort = req.Email.SMTPPort
		}
		if req.Email.Username != "" {
			cfg.Channels.Email.Username = req.Email.Username
		}
		if req.Email.Password != "" && !strings.HasPrefix(req.Email.Password, "••") {
			cfg.Channels.Email.Password = req.Email.Password
			updateEnvFile("GHOST_CHANNELS_EMAIL_PASSWORD", req.Email.Password)
		}
		if req.Email.From != "" {
			cfg.Channels.Email.From = req.Email.From
		}
		if req.Email.To != "" {
			cfg.Channels.Email.To = req.Email.To
		}
	}
	if req.Heartbeat != nil {
		cfg.Heartbeat.Enabled = req.Heartbeat.Enabled
		if req.Heartbeat.Interval >= 5 {
			cfg.Heartbeat.Interval = req.Heartbeat.Interval
		}
	}

	if err := config.SaveConfig(fb.ConfigPath, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Channel settings saved"})
}

// ---------- Phase 4: System admin ----------

func handleNetworkStatus(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	hostname, _ := os.Hostname()
	ip := appliance.GetLocalIP()

	connected := ""
	wifi := "unknown"
	if out, err := exec.Command("nmcli", "-t", "-f", "DEVICE,TYPE,STATE,CONNECTION", "device", "status").CombinedOutput(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			parts := strings.Split(line, ":")
			if len(parts) >= 4 && parts[1] == "wifi" {
				wifi = parts[2]
				connected = parts[3]
				break
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":        true,
		"hostname":  hostname,
		"ip":        ip,
		"wifi":      wifi,
		"connected": connected,
	})
}

func handleSetHostname(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Hostname string `json:"hostname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Hostname == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "hostname is required"})
		return
	}
	if err := os.WriteFile("/etc/hostname", []byte(req.Hostname+"\n"), 0644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	out, err := runPrivileged("hostnamectl", "set-hostname", req.Hostname)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": strings.TrimSpace(string(out))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Hostname updated"})
}

func handleBackup(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filename := fmt.Sprintf("ghost-backup-%s.tar.gz", time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	// Add config, data, workspace, and .env
	dirs := []string{fb.ConfigDir, fb.DataDir, fb.Workspace}
	_ = filepath.Walk(fb.GhostDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// Skip workspace internals that change constantly.
		rel, _ := filepath.Rel(fb.GhostDir, path)
		if info.IsDir() {
			for _, skip := range []string{"journal", "state", "sessions"} {
				if filepath.Base(path) == skip {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasPrefix(rel, ".env") {
			return nil // handled separately below
		}
		ok := false
		for _, d := range dirs {
			if strings.HasPrefix(path, d) {
				ok = true
				break
			}
		}
		if !ok {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return nil
		}
		hdr.Name = "ghost/" + rel
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		io.Copy(tw, f)
		f.Close()
		return nil
	})

	if b, err := os.ReadFile(fb.EnvPath); err == nil {
		hdr := &tar.Header{Name: "ghost/.env", Mode: 0600, Size: int64(len(b))}
		tw.WriteHeader(hdr)
		tw.Write(b)
	}

	tw.Close()
	gz.Close()
}

func handleReboot(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	go func() {
		time.Sleep(2 * time.Second)
		runPrivileged("reboot")
	}()
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Rebooting..."})
}

func handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid request"})
		return
	}
	if len(req.New) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "new password must be at least 8 characters"})
		return
	}
	ok, err := appliance.VerifyAdminPassword(fb.GhostDir, req.Current)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "failed to verify current password"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "error": "current password is incorrect"})
		return
	}
	if err := appliance.SetAdminPassword(fb.GhostDir, req.New); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Password updated"})
}

func handleRegenBridge(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	secret, err := appliance.GenerateBridgeSecret()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	cfg, err := config.LoadConfig(fb.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	cfg.Gateway.BridgeSecret = secret
	if err := config.SaveConfig(fb.ConfigPath, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	if err := updateEnvFile("BRIDGE_SECRET", secret); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	go restartGhostService()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"message": "Bridge secret regenerated. Re-pair your app with the new secret.",
		"secret":  secret,
	})
}

// ---------- Skills ----------

func workspaceSkillsDir() string {
	return filepath.Join(fb.Workspace, "skills")
}

func handleSkillsList(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	skillsDir := workspaceSkillsDir()
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "skills": []map[string]string{}})
		return
	}
	skills := []map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// A skill is a directory containing SKILL.md.
		if _, err := os.Stat(filepath.Join(skillsDir, name, "SKILL.md")); err != nil {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(skillsDir, name, "SKILL.md"))
		desc := skillSummary(string(b))
		if len(desc) > 120 {
			desc = desc[:120] + "..."
		}
		skills = append(skills, map[string]string{"name": name, "description": desc})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i]["name"] < skills[j]["name"] })
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "skills": skills})
}

// gitHubRepoTree lists blob paths under prefix for owner/repo on branch.
func gitHubRepoTree(owner, repo, branch, prefix string) ([]string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, branch)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("skills.sh: repo lookup failed (HTTP %d)", resp.StatusCode)
	}
	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return nil, err
	}
	var paths []string
	for _, t := range tree.Tree {
		if t.Type == "blob" && strings.HasPrefix(t.Path, prefix) {
			paths = append(paths, t.Path)
		}
	}
	return paths, nil
}

// installSkillFromGitHub downloads a skill directory into the workspace skills dir.
func installSkillFromGitHub(owner, repo, branch, prefix, destName string) error {
	paths, err := gitHubRepoTree(owner, repo, branch, prefix)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no skill files found at %s/%s", repo, prefix)
	}

	dest := filepath.Join(workspaceSkillsDir(), destName)
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("skill '%s' already exists", destName)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	for _, p := range paths {
		url := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, branch, p)
		resp, err := client.Get(url)
		if err != nil {
			return err
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return fmt.Errorf("failed to download %s (HTTP %d)", p, resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, prefix)
		rel = strings.TrimPrefix(rel, "/")
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(target, body, 0644); err != nil {
			return err
		}
	}
	return nil
}

func handleSkillInstall(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Owner  string `json:"owner"`
		Repo   string `json:"repo"`
		Path   string `json:"path"`
		Name   string `json:"name"`
		Branch string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid request"})
		return
	}
	if req.Owner == "" || req.Repo == "" || req.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "owner, repo and path are required"})
		return
	}
	if req.Name == "" {
		req.Name = filepath.Base(strings.TrimSuffix(req.Path, "/"))
	}
	if req.Branch == "" {
		req.Branch = "main"
	}

	if err := installSkillFromGitHub(req.Owner, req.Repo, req.Branch, req.Path, req.Name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Skill installed: " + req.Name})
}

func handleSkillRemove(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "name is required"})
		return
	}
	target := filepath.Join(workspaceSkillsDir(), req.Name)
	if !strings.HasPrefix(target, workspaceSkillsDir()) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid skill name"})
		return
	}
	if err := os.RemoveAll(target); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Skill removed"})
}

// startTime is recorded when the wizard process starts.
var startTime = time.Now()

// systemUptime returns the system uptime as a human-readable string.
// Reads /proc/uptime on Linux, falls back to wizard process uptime.
func systemUptime() string {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return time.Since(startTime).String()
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return time.Since(startTime).String()
	}
	uptimeSec, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return time.Since(startTime).String()
	}
	d := time.Duration(uptimeSec * float64(time.Second))
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

// skillSummary extracts a one-line description from a SKILL.md file, preferring
// the frontmatter description field and falling back to the first content line.
func skillSummary(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "---" {
			continue
		}
		if strings.HasPrefix(line, "description:") {
			desc := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			desc = strings.Trim(desc, "\"'")
			if desc != "" {
				return desc
			}
		}
	}
	// Fall back to the first non-empty content line, skipping frontmatter.
	if strings.HasPrefix(strings.TrimSpace(text), "---") {
		if idx := strings.Index(text[3:], "\n---"); idx != -1 {
			text = text[3+idx+4:]
		}
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
		line = strings.TrimSpace(strings.TrimPrefix(line, ">"))
		if line != "" && !strings.HasPrefix(line, "-") {
			if len(line) > 200 {
				return line[:200]
			}
			return line
		}
	}
	return ""
}
