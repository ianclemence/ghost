package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ianclemence/ghost/pkg/appliance"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/skills"
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
// On success it bumps last_seen so the sessions list stays accurate.
func requireSession(w http.ResponseWriter, r *http.Request) bool {
	tok := sessionToken(r)
	if !sessions.valid(tok) {
		http.Error(w, `{"ok":false,"error":"session expired, please log in"}`, http.StatusUnauthorized)
		return false
	}
	sessions.touch(tok)
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

func handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
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
	for _, name := range []string{"ghost", "ghost-web", "ollama"} {
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
		"ok":          true,
		"hostname":    hostname,
		"version":     version,
		"uptime":      systemUptime(),
		"ip":          ip,
		"model":       model,
		"provider":    provider,
		"ollama_url":  ollamaURL,
		"cpu_percent": cpuUsagePercent(),
		"cpu_count":   runtime.NumCPU(),
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

	cfg, _ := config.LoadConfig(fb.ConfigPath)

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

// ---------- Auth metadata ----------

func handleAdminMeta(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	meta := appliance.LoadAdminMeta(fb.GhostDir)
	ownerName := ""
	if id, err := ghoststate.LoadIdentity(fb.Workspace); err == nil && id != nil {
		ownerName = id.OwnerName
	}
	if meta == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":         true,
			"configured": false,
			"owner_name": ownerName,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":           true,
		"configured":   true,
		"owner_name":   ownerName,
		"created_at":   meta.CreatedAt,
		"last_changed": meta.LastChanged,
	})
}

// handleAdminIdentity returns the Ghost's persistent identity: the owner
// name, the Ghost's display name, the identity created-at timestamp, and
// the workspace directory it lives in. Used by the About section.
func handleAdminIdentity(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	resp := map[string]interface{}{
		"ok":         true,
		"configured": false,
	}
	if id, err := ghoststate.LoadIdentity(fb.Workspace); err == nil && id != nil && id.GhostID != "" {
		resp["configured"] = true
		resp["ghost_id"] = id.GhostID
		resp["ghost_name"] = id.GhostName
		resp["owner_name"] = id.OwnerName
		if id.CreatedAt != "" {
			resp["created_at"] = id.CreatedAt
		}
	}
	if meta := appliance.LoadAdminMeta(fb.GhostDir); meta != nil {
		if !meta.CreatedAt.IsZero() && resp["created_at"] == nil {
			resp["created_at"] = meta.CreatedAt.UTC().Format(time.RFC3339)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleAdminSessions lists active admin sessions for the Security section.
// GET /api/admin/sessions
func handleAdminSessions(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	currentToken := sessionToken(r)
	records := sessions.list()
	type sessionJSON struct {
		Token     string `json:"token"`
		Current   bool   `json:"current"`
		IssuedAt  string `json:"issued_at"`
		ExpiresAt string `json:"expires_at"`
		LastSeen  string `json:"last_seen"`
		IP        string `json:"ip"`
		UserAgent string `json:"user_agent"`
	}
	out := make([]sessionJSON, 0, len(records))
	for _, rec := range records {
		out = append(out, sessionJSON{
			Token:     rec.Token,
			Current:   rec.Token == currentToken,
			IssuedAt:  rec.IssuedAt.Format(time.RFC3339),
			ExpiresAt: rec.ExpiresAt.Format(time.RFC3339),
			LastSeen:  rec.LastSeen.Format(time.RFC3339),
			IP:        rec.IP,
			UserAgent: rec.UserAgent,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"sessions": out,
	})
}

// handleAdminSessionRevoke signs out a single session by token, or all
// sessions except the current one when target=all.
func handleAdminSessionRevoke(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Token  string `json:"token"`
		Action string `json:"action"` // "revoke" | "revoke_all"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid request"})
		return
	}
	currentToken := sessionToken(r)
	if req.Action == "revoke_all" {
		// Revoke every session except the current one.
		for _, rec := range sessions.list() {
			if rec.Token != currentToken {
				sessions.revoke(rec.Token)
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "other sessions signed out"})
		return
	}
	if req.Token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "token required"})
		return
	}
	if req.Token == currentToken {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "cannot revoke your own session; use sign out"})
		return
	}
	sessions.revoke(req.Token)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "session signed out"})
}

func handleFailedLogins(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"attempts": getRecentFailedLogins(),
	})
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
		"moonshot":   {"api_key": maskKey(cfg.Providers.Moonshot.APIKey), "api_base": cfg.Providers.Moonshot.APIBase},
		"anthropic":  {"api_key": maskKey(cfg.Providers.Anthropic.APIKey), "api_base": cfg.Providers.Anthropic.APIBase},
		"openai":     {"api_key": maskKey(cfg.Providers.OpenAI.APIKey), "api_base": cfg.Providers.OpenAI.APIBase},
		"openrouter": {"api_key": maskKey(cfg.Providers.OpenRouter.APIKey), "api_base": cfg.Providers.OpenRouter.APIBase},
		"groq":       {"api_key": maskKey(cfg.Providers.Groq.APIKey), "api_base": cfg.Providers.Groq.APIBase},
		"deepseek":   {"api_key": maskKey(cfg.Providers.DeepSeek.APIKey), "api_base": cfg.Providers.DeepSeek.APIBase},
		"qwen":       {"api_key": maskKey(cfg.Providers.Qwen.APIKey), "api_base": cfg.Providers.Qwen.APIBase},
		"gemini":     {"api_key": maskKey(cfg.Providers.Gemini.APIKey), "api_base": cfg.Providers.Gemini.APIBase},
		"zhipu":      {"api_key": maskKey(cfg.Providers.Zhipu.APIKey), "api_base": cfg.Providers.Zhipu.APIBase},
		"ollama":     {"api_key": maskKey(cfg.Providers.Ollama.APIKey), "api_base": cfg.Providers.Ollama.APIBase},
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":              true,
		"provider":        cfg.Agents.Defaults.Provider,
		"model":           cfg.Agents.Defaults.Model,
		"fallback_models": cfg.Agents.Defaults.FallbackModels,
		"embedding_model": cfg.Agents.Defaults.EmbeddingModel,
		"model_list":      cfg.Agents.ModelList,
		"max_tokens":      cfg.Agents.Defaults.MaxTokens,
		"temperature":     cfg.Agents.Defaults.Temperature,
		"providers":       providers,
		"routing": map[string]interface{}{
			"prefer_local":           cfg.Agents.Routing.PreferLocal,
			"allow_cloud":            cfg.Agents.Routing.AllowCloud,
			"cloud_when_local_fails": cfg.Agents.Routing.CloudWhenLocalFails,
		},
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
		Provider       string               `json:"provider"`
		Model          string               `json:"model"`
		FallbackModels []string             `json:"fallback_models"`
		EmbeddingModel string               `json:"embedding_model"`
		OllamaURL      string               `json:"ollama_url"`
		APIKeys        map[string]string    `json:"api_keys"`
		MaxTokens      int                  `json:"max_tokens"`
		Temperature    float64              `json:"temperature"`
		ModelList      []config.ModelPreset `json:"model_list"`
		Routing        *struct {
			PreferLocal         bool `json:"prefer_local"`
			AllowCloud          bool `json:"allow_cloud"`
			CloudWhenLocalFails bool `json:"cloud_when_local_fails"`
		} `json:"routing"`
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

	// API keys: write to config.json for persistence via .secrets.json.
	// Do NOT write to .env — secrets belong in .secrets.json only.
	cfgKeys := map[string]*config.ProviderConfig{
		"moonshot":   &cfg.Providers.Moonshot,
		"anthropic":  &cfg.Providers.Anthropic,
		"openai":     &cfg.Providers.OpenAI,
		"openrouter": &cfg.Providers.OpenRouter,
		"groq":       &cfg.Providers.Groq,
		"deepseek":   &cfg.Providers.DeepSeek,
		"gemini":     &cfg.Providers.Gemini,
		"zhipu":      &cfg.Providers.Zhipu,
	}
	for name, key := range req.APIKeys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
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
	if req.ModelList != nil {
		cfg.Agents.ModelList = req.ModelList
	}
	if req.Routing != nil {
		cfg.Agents.Routing.PreferLocal = req.Routing.PreferLocal
		cfg.Agents.Routing.AllowCloud = req.Routing.AllowCloud
		cfg.Agents.Routing.CloudWhenLocalFails = req.Routing.CloudWhenLocalFails
	}

	if err := config.SaveConfig(fb.ConfigPath, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "AI configuration saved"})
}

// ---------- Provider models & testing ----------

// knownModels returns the recommended models for each provider.
// These come from the factory.go validation logic and provider docs.
var knownModels = map[string][]string{
	"openai":       {"gpt-5.4", "gpt-5.4-mini", "gpt-5", "gpt-5-mini", "gpt-4.1", "gpt-4.1-mini", "gpt-4.1-nano", "o3", "o4-mini", "gpt-4o", "gpt-4o-mini"},
	"anthropic":    {"claude-fable-5", "claude-opus-5", "claude-sonnet-5", "claude-haiku-4-5", "claude-sonnet-4-6", "claude-opus-4-6"},
	"moonshot":     {"kimi-k3", "kimi-k2.7-code", "kimi-k2.6"},
	"groq":         {"llama-3.3-70b-versatile", "llama-3.1-8b-instant", "openai/gpt-oss-120b", "openai/gpt-oss-20b", "qwen/qwen3.6-27b"},
	"deepseek":     {"deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp"},
	"qwen":         {"qwen3.8-max", "qwen3.7-plus", "qwen3.8-flash", "qwen3.5-omni-plus"},
	"gemini":       {"gemini-3.6-flash", "gemini-3.1-pro", "gemini-3-flash"},
	"zhipu":        {"glm-5.3", "glm-5.3-flash", "glm-5.2", "glm-4.7", "glm-4.7-flash"},
	"openrouter":   {},
	"ollama":       {},
	"nvidia":       {"deepseek-ai/deepseek-v4-flash", "meta/llama-3.3-70b-instruct", "qwen/qwq-32b"},
	"shengsuanyun": {},
}

func handleProviderModels(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	cfg, err := config.LoadConfig(fb.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	// For Ollama, list actual installed models
	ollamaModels := []string{}
	if models, err := listOllamaModels(); err == nil {
		ollamaModels = models
	}

	providers := map[string]interface{}{}
	for name, models := range knownModels {
		pc := getProviderConfig(cfg, name)
		configured := pc != nil && pc.APIKey != ""
		providerModels := models
		if name == "ollama" {
			providerModels = ollamaModels
		}
		providers[name] = map[string]interface{}{
			"configured": configured,
			"models":     providerModels,
			"local":      name == "ollama" || name == "vllm",
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":        true,
		"provider":  cfg.Agents.Defaults.Provider,
		"model":     cfg.Agents.Defaults.Model,
		"providers": providers,
	})
}

// getProviderConfig returns the ProviderConfig for a given provider name.
func getProviderConfig(cfg *config.Config, name string) *config.ProviderConfig {
	switch name {
	case "openai":
		return &cfg.Providers.OpenAI
	case "anthropic":
		return &cfg.Providers.Anthropic
	case "moonshot":
		return &cfg.Providers.Moonshot
	case "groq":
		return &cfg.Providers.Groq
	case "deepseek":
		return &cfg.Providers.DeepSeek
	case "gemini":
		return &cfg.Providers.Gemini
	case "zhipu":
		return &cfg.Providers.Zhipu
	case "openrouter":
		return &cfg.Providers.OpenRouter
	case "ollama":
		return &cfg.Providers.Ollama
	case "qwen":
		return &cfg.Providers.Qwen
	case "nvidia":
		return &cfg.Providers.Nvidia
	case "shengsuanyun":
		return &cfg.Providers.ShengSuanYun
	}
	return nil
}

// createProviderForConfig creates an LLM provider from a config.
func createProviderForConfig(cfg *config.Config) (providers.LLMProvider, error) {
	return providers.CreateProvider(cfg)
}

func handleProviderTest(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Provider string `json:"provider"`
		APIKey   string `json:"api_key"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid request"})
		return
	}

	// Build a temporary config with the provided key to test
	cfg, err := config.LoadConfig(fb.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	pc := getProviderConfig(cfg, req.Provider)
	if pc == nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "unknown provider"})
		return
	}
	if req.APIKey != "" {
		pc.APIKey = req.APIKey
	}

	// Pick a test model
	testModel := req.Model
	if testModel == "" {
		if models, ok := knownModels[req.Provider]; ok && len(models) > 0 {
			testModel = models[0]
		}
	}
	if testModel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "no model to test"})
		return
	}

	// Create a provider and attempt a minimal chat
	cfg.Agents.Defaults.Provider = req.Provider
	cfg.Agents.Defaults.Model = testModel
	p, err := createProviderForConfig(cfg)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      false,
			"status":  "error",
			"message": fmt.Sprintf("Couldn\u2019t create provider: %v", err),
		})
		return
	}

	// Send a minimal test message with a short timeout
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	_, err = p.Chat(ctx, []providers.Message{
		{Role: "user", Content: "Reply with only the word OK"},
	}, nil, testModel, map[string]interface{}{
		"max_tokens":  30,
		"temperature": 0,
	})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      false,
			"status":  "error",
			"message": fmt.Sprintf("Connection failed: %v", err),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"status":  "ok",
		"message": "Connected successfully",
	})
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
				"enabled":   cfg.Channels.Slack.Enabled,
				"bot_token": maskKey(cfg.Channels.Slack.BotToken),
				"app_token": maskKey(cfg.Channels.Slack.AppToken),
			},
			"email": map[string]interface{}{
				"enabled":   cfg.Channels.Email.Enabled,
				"smtp_host": cfg.Channels.Email.SMTPHost,
				"smtp_port": cfg.Channels.Email.SMTPPort,
				"username":  cfg.Channels.Email.Username,
				"from":      cfg.Channels.Email.From,
				"to":        cfg.Channels.Email.To,
			},
			"whatsapp": map[string]interface{}{
				"enabled":    cfg.Channels.WhatsApp.Enabled,
				"bridge_url": cfg.Channels.WhatsApp.BridgeURL,
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
		Slack *struct {
			Enabled  bool   `json:"enabled"`
			BotToken string `json:"bot_token"`
			AppToken string `json:"app_token"`
		} `json:"slack"`
		WhatsApp *struct {
			Enabled   bool   `json:"enabled"`
			BridgeURL string `json:"bridge_url"`
		} `json:"whatsapp"`
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
		}
	}
	if req.Discord != nil {
		cfg.Channels.Discord.Enabled = req.Discord.Enabled
		if req.Discord.Token != "" && !strings.HasPrefix(req.Discord.Token, "••") {
			cfg.Channels.Discord.Token = req.Discord.Token
		}
	}
	if req.Slack != nil {
		cfg.Channels.Slack.Enabled = req.Slack.Enabled
		if req.Slack.BotToken != "" && !strings.HasPrefix(req.Slack.BotToken, "••") {
			cfg.Channels.Slack.BotToken = req.Slack.BotToken
		}
		if req.Slack.AppToken != "" && !strings.HasPrefix(req.Slack.AppToken, "••") {
			cfg.Channels.Slack.AppToken = req.Slack.AppToken
		}
	}
	if req.WhatsApp != nil {
		cfg.Channels.WhatsApp.Enabled = req.WhatsApp.Enabled
		if req.WhatsApp.BridgeURL != "" {
			cfg.Channels.WhatsApp.BridgeURL = req.WhatsApp.BridgeURL
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

	// Add config, data, workspace, and .env. The runtime workspace may live
	// outside the install tree (see the workspace migration), so walk both
	// roots and give each its own archive prefix.
	dirs := []string{fb.ConfigDir, fb.DataDir, fb.Workspace}
	roots := []string{fb.GhostDir}
	if fb.Workspace != filepath.Join(fb.GhostDir, "workspace") {
		roots = append(roots, filepath.Dir(fb.Workspace))
	}
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			// Skip workspace internals that change constantly.
			rel, _ := filepath.Rel(root, path)
			if info.IsDir() {
				for _, skip := range []string{"journal", "state", "sessions"} {
					if filepath.Base(path) == skip {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if strings.HasPrefix(rel, ".env") {
				return nil // secrets — never included in backups
			}
			if strings.HasSuffix(path, ".secrets.json") {
				return nil // credentials — never included in backups
			}
			if strings.HasSuffix(path, ".gcalcli_oauth") || strings.Contains(path, "gcalcli"+string(filepath.Separator)+"oauth") {
				return nil // calendar OAuth tokens — never included in backups
			}
			if strings.HasSuffix(path, ".log") {
				return nil // transient logs
			}
			ok := false
			for _, d := range dirs {
				if path == d || strings.HasPrefix(path, d+string(filepath.Separator)) {
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

// handleRestartGhost restarts only the Ghost (gateway) service, not the device.
// This is the normal-user, safe way to recover from a stuck Ghost without
// needing a terminal.
func handleRestartGhost(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	restartGhostService()
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Ghost is restarting"})
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
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid request"})
		return
	}
	if req.New != req.Confirm {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "new passwords do not match"})
		return
	}
	if err := appliance.ValidatePassword(req.New); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": err.Error()})
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

// ---------- Personality ----------

func handlePersonalityGet(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	cfg, err := config.LoadConfig(fb.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	builtins := []map[string]string{
		{"name": "default", "description": "Professional, concise, helpful"},
		{"name": "hacker", "description": "Technical deep-dive, code-first"},
		{"name": "creative", "description": "Brainstorming, writing, ideation"},
		{"name": "teacher", "description": "Patient educator, step-by-step"},
		{"name": "minimal", "description": "Ultra-concise, one-line answers"},
	}

	// Scan for custom personalities in ~/.GHOST/personalities/
	customDir := filepath.Join(fb.GhostDir, "personalities")
	var custom []map[string]string
	if entries, err := os.ReadDir(customDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(customDir, e.Name()))
			if err != nil {
				continue
			}
			var p struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			}
			if json.Unmarshal(b, &p) == nil && p.Name != "" {
				custom = append(custom, map[string]string{"name": p.Name, "description": p.Description})
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"active":   cfg.Personality.Active,
		"builtins": builtins,
		"custom":   custom,
	})
}

func handlePersonalitySet(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Active string `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid request"})
		return
	}
	if req.Active == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "active personality is required"})
		return
	}

	cfg, err := config.LoadConfig(fb.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	cfg.Personality.Active = req.Active
	if err := config.SaveConfig(fb.ConfigPath, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Personality set to " + req.Active})
}

func handlePersonalityCreate(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid request"})
		return
	}
	if req.Name == "" || req.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "name and content are required"})
		return
	}

	customDir := filepath.Join(fb.GhostDir, "personalities")
	if err := os.MkdirAll(customDir, 0755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	p := map[string]string{
		"name":        strings.ToLower(strings.TrimSpace(req.Name)),
		"description": req.Description,
		"content":     req.Content,
	}
	data, _ := json.MarshalIndent(p, "", "  ")
	path := filepath.Join(customDir, p["name"]+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Custom personality created: " + p["name"]})
}

func handlePersonalityDelete(w http.ResponseWriter, r *http.Request) {
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

	builtins := map[string]bool{"default": true, "hacker": true, "creative": true, "teacher": true, "minimal": true}
	if builtins[req.Name] {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "cannot delete builtin personality"})
		return
	}

	path := filepath.Join(fb.GhostDir, "personalities", req.Name+".json")
	if err := os.Remove(path); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"ok": false, "error": "personality not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Personality deleted"})
}

// ---------- Logs ----------

func handleLogs(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}

	lines := 100
	if l := r.URL.Query().Get("lines"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 500 {
			lines = v
		}
	}

	// Try journalctl for ghost service logs, fallback to syslog
	var out []byte
	var err error
	out, err = exec.Command("journalctl", "-u", "ghost", "-n", strconv.Itoa(lines), "--no-pager", "-o", "short-iso").CombinedOutput()
	if err != nil {
		// Fallback: try reading from /var/log/syslog for ghost entries
		out, err = exec.Command("tail", "-n", strconv.Itoa(lines), "/var/log/syslog").CombinedOutput()
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"ok":    true,
				"logs":  []string{},
				"error": "no log source available",
			})
			return
		}
	}

	logLines := strings.Split(strings.TrimSpace(string(out)), "\n")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":   true,
		"logs": logLines,
	})
}

// ---------- Toolsets ----------

func handleToolsetsGet(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	cfg, err := config.LoadConfig(fb.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	// Default toolsets
	toolsets := []map[string]interface{}{
		{"name": "default", "description": "Standard tools: memory, web search, file access", "tools": "memory,web_search,read,write,edit,list_files,glob,grep"},
		{"name": "minimal", "description": "Minimal tools: chat only, no external access", "tools": "memory"},
		{"name": "developer", "description": "Full developer tools: code, files, web, terminal", "tools": "memory,web_search,read,write,edit,list_files,glob,grep,terminal,web_fetch"},
		{"name": "researcher", "description": "Research tools: web search, reading, memory", "tools": "memory,web_search,web_fetch,read,list_files,glob,grep"},
		{"name": "creator", "description": "Content creation: files, web, code generation", "tools": "memory,web_search,web_fetch,read,write,edit,list_files,glob,grep,terminal"},
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"active":   cfg.Toolsets.Active,
		"toolsets": toolsets,
	})
}

func handleToolsetsSet(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Active string `json:"active"`
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

	cfg.Toolsets.Active = req.Active
	if err := config.SaveConfig(fb.ConfigPath, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Toolset set to " + req.Active})
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
	manifest, _ := skills.LoadManifest(skillsDir)
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
		entry, bundled := manifest.Skills[name]
		enabled := true
		if _, err := os.Stat(filepath.Join(skillsDir, name, "SKILL.md.disabled")); err == nil {
			enabled = false
		}
		skills = append(skills, map[string]string{
			"name":          name,
			"description":   desc,
			"bundled":       strconv.FormatBool(bundled),
			"user_modified": strconv.FormatBool(bundled && entry.UserModified),
			"enabled":       strconv.FormatBool(enabled),
		})
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

// installSkillNameRE allows letters, numbers, hyphen, underscore only.
// It mirrors the gateway's validSkillName guard so both backends agree.
var installSkillNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func validInstallSkillName(name string) bool {
	return installSkillNameRE.MatchString(name)
}

// blockedSkillExts are never written for custom installs (executables,
// libraries, archives that could hide payloads outside review).
var blockedSkillExts = map[string]bool{
	".sh": true, ".exe": true, ".bin": true, ".so": true, ".dylib": true,
	".dll": true, ".zip": true, ".tar": true, ".gz": true,
}

// installSkillFromGitHub downloads a skill directory into the workspace skills dir.
// It validates the name, caps file count/size, blocks risky extensions, and
// verifies the result contains a valid SKILL.md (name + description) before
// reporting success — so a broken install never looks ready.
func installSkillFromGitHub(owner, repo, branch, prefix, destName string) error {
	if !validInstallSkillName(destName) {
		return fmt.Errorf("invalid skill name %q: use letters, numbers, hyphen, underscore", destName)
	}
	paths, err := gitHubRepoTree(owner, repo, branch, prefix)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no skill files found at %s/%s", repo, prefix)
	}
	if len(paths) > 50 {
		return fmt.Errorf("skill too large (%d files, max 50)", len(paths))
	}

	dest := filepath.Join(workspaceSkillsDir(), destName)
	if !strings.HasPrefix(dest, workspaceSkillsDir()+string(filepath.Separator)) && dest != workspaceSkillsDir() {
		return fmt.Errorf("invalid skill name")
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("skill '%s' already exists", destName)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	var total int64
	downloaded := map[string][]byte{}
	for _, p := range paths {
		rel := strings.TrimPrefix(p, prefix)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" || strings.Contains(rel, "..") || filepath.IsAbs(rel) {
			return fmt.Errorf("unsafe path in skill: %q", p)
		}
		if ext := strings.ToLower(filepath.Ext(rel)); blockedSkillExts[ext] {
			return fmt.Errorf("blocked file type in skill: %q", rel)
		}
		url := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, branch, p)
		resp, err := client.Get(url)
		if err != nil {
			return err
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return fmt.Errorf("failed to download %s (HTTP %d)", p, resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 200*1024))
		resp.Body.Close()
		if err != nil {
			return err
		}
		total += int64(len(body))
		if total > 1024*1024 {
			return fmt.Errorf("skill too large (over 1MB)")
		}
		downloaded[rel] = body
	}
	// Verify SKILL.md with valid frontmatter before writing anything.
	skillMD, ok := downloaded["SKILL.md"]
	if !ok {
		return fmt.Errorf("skill is missing SKILL.md at its folder root")
	}
	name, desc := parseSkillFrontmatter(string(skillMD))
	if name == "" || desc == "" {
		return fmt.Errorf("SKILL.md needs name and description in its frontmatter")
	}
	for rel, body := range downloaded {
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if !strings.HasPrefix(target, dest+string(filepath.Separator)) && target != dest {
			return fmt.Errorf("unsafe path in skill: %q", rel)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(target, body, 0644); err != nil {
			return err
		}
	}
	return nil
}

// parseSkillFrontmatter extracts name + description from SKILL.md frontmatter
// (same --- block the gateway loader reads).
func parseSkillFrontmatter(text string) (name, desc string) {
	rest := strings.TrimSpace(text)
	if !strings.HasPrefix(rest, "---") {
		return "", ""
	}
	end := strings.Index(rest[3:], "\n---")
	if end < 0 {
		return "", ""
	}
	for _, line := range strings.Split(rest[3:3+end], "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			name = strings.Trim(strings.TrimPrefix(line, "name:"), " \"'")
		}
		if strings.HasPrefix(line, "description:") {
			desc = strings.Trim(strings.TrimPrefix(line, "description:"), " \"'")
		}
	}
	return name, desc
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

func handleSkillToggle(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "name is required"})
		return
	}

	dir := workspaceSkillsDir()
	skillDir := filepath.Join(dir, req.Name)
	if !strings.HasPrefix(skillDir, dir) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid skill name"})
		return
	}

	src := filepath.Join(skillDir, "SKILL.md")
	dst := filepath.Join(skillDir, "SKILL.md.disabled")

	if req.Enabled {
		// Enable: rename .disabled back to SKILL.md
		if _, err := os.Stat(dst); err == nil {
			if err := os.Rename(dst, src); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Skill enabled"})
	} else {
		// Disable: rename SKILL.md to .disabled
		if _, err := os.Stat(src); err == nil {
			if err := os.Rename(src, dst); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Skill disabled"})
	}
}

// bundledSkillsSourceDir resolves where the bundled skills live for this
// appliance. Overridable for testing and for setups where the bundled copy is
// kept separately from the runtime workspace. On installed layouts the
// runtime workspace lives outside the install tree (see workspace migration),
// so there is no bundled copy here — the ghost binary embeds it and seeds the
// runtime workspace on first start instead. Returns "" when no source exists.
func bundledSkillsSourceDir() string {
	if d := os.Getenv("GHOST_BUNDLED_SKILLS"); d != "" {
		return d
	}
	if _, err := os.Stat(filepath.Join(fb.GhostDir, "workspace", "skills")); err == nil {
		return filepath.Join(fb.GhostDir, "workspace", "skills")
	}
	return ""
}

func handleSkillsSync(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	src := bundledSkillsSourceDir()
	if src == "" {
		// No bundled copy on this device (installed layout). The gateway
		// seeds bundled skills from its embedded copy on first start, so
		// there is nothing to reconcile here yet.
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "report": "no bundled source on this device; the gateway seeds bundled skills from its embedded copy"})
		return
	}
	report, err := skills.SyncBundled(src, workspaceSkillsDir())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "report": report})
}

func handleSkillRead(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.URL.Query().Get("name")
	skillsDir := workspaceSkillsDir()
	skillDir := filepath.Join(skillsDir, name)
	if !strings.HasPrefix(skillDir, skillsDir) || name == "" || strings.Contains(name, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid skill name"})
		return
	}
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "skill not found"})
		return
	}
	enabled := true
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md.disabled")); err == nil {
		enabled = false
	}
	manifest, _ := skills.LoadManifest(skillsDir)
	entry, bundled := manifest.Skills[name]
	files := []map[string]string{}
	_ = filepath.Walk(skillDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if info.Name() == skills.BundledManifestFile {
			return nil
		}
		rel, err := filepath.Rel(skillDir, path)
		if err != nil {
			return nil
		}
		b, _ := os.ReadFile(path)
		files = append(files, map[string]string{"path": filepath.ToSlash(rel), "content": string(b)})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i]["path"] < files[j]["path"] })
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":            true,
		"name":          name,
		"bundled":       bundled,
		"enabled":       enabled,
		"user_modified": bundled && entry.UserModified,
		"files":         files,
	})
}

func handleSkillSave(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name  string `json:"name"`
		Files []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		} `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid request"})
		return
	}
	skillsDir := workspaceSkillsDir()
	skillDir := filepath.Join(skillsDir, req.Name)
	if !strings.HasPrefix(skillDir, skillsDir) || req.Name == "" || strings.Contains(req.Name, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid skill name"})
		return
	}
	for _, f := range req.Files {
		target := filepath.Join(skillDir, filepath.FromSlash(f.Path))
		if !strings.HasPrefix(target, skillDir) {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid file path"})
			return
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		if err := os.WriteFile(target, []byte(f.Content), 0644); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
	}
	// Editing marks the skill as user-modified: future bundled syncs will
	// never overwrite it.
	if err := skills.MarkUserModified(skillsDir, req.Name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Skill saved"})
}

func handleClawHubSearch(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	query := r.URL.Query().Get("q")
	if query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "query is required"})
		return
	}

	cfg, err := config.LoadConfig(fb.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	ctx := r.Context()
	registry := newClawHubRegistry(cfg)
	results, err := registry.Search(ctx, query, 20)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"results": results,
	})
}

func handleClawHubInstall(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Slug    string `json:"slug"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Slug == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "slug is required"})
		return
	}

	cfg, err := config.LoadConfig(fb.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	ctx := r.Context()
	registry := newClawHubRegistry(cfg)
	targetDir := workspaceSkillsDir()
	result, err := registry.DownloadAndInstall(ctx, req.Slug, req.Version, targetDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"message": "Skill installed from ClawHub",
		"version": result,
	})
}

// newClawHubRegistry creates a ClawHub registry client from config.
func newClawHubRegistry(cfg *config.Config) *clawHubClient {
	baseURL := cfg.Skills.ClawHub.BaseURL
	if baseURL == "" {
		baseURL = "https://clawhub.ai"
	}
	searchPath := cfg.Skills.ClawHub.SearchPath
	if searchPath == "" {
		searchPath = "/api/v1/search"
	}
	skillsPath := cfg.Skills.ClawHub.SkillsPath
	if skillsPath == "" {
		skillsPath = "/api/v1/skills"
	}
	downloadPath := cfg.Skills.ClawHub.DownloadPath
	if downloadPath == "" {
		downloadPath = "/api/v1/download"
	}
	timeout := 30 * time.Second
	if cfg.Skills.ClawHub.Timeout > 0 {
		timeout = time.Duration(cfg.Skills.ClawHub.Timeout) * time.Second
	}
	return &clawHubClient{
		baseURL:      baseURL,
		authToken:    cfg.Skills.ClawHub.AuthToken,
		searchPath:   searchPath,
		skillsPath:   skillsPath,
		downloadPath: downloadPath,
		client:       &http.Client{Timeout: timeout},
	}
}

// clawHubClient is a lightweight ClawHub HTTP client for the admin API.
type clawHubClient struct {
	baseURL, authToken, searchPath, skillsPath, downloadPath string
	client                                                   *http.Client
}

type clawHubSearchResp struct {
	Results []struct {
		Score       float64 `json:"score"`
		Slug        *string `json:"slug"`
		DisplayName *string `json:"displayName"`
		Summary     *string `json:"summary"`
		Version     *string `json:"version"`
	} `json:"results"`
}

func derefStr(s *string, def string) string {
	if s != nil {
		return *s
	}
	return def
}

func (c *clawHubClient) Search(ctx context.Context, query string, limit int) ([]map[string]interface{}, error) {
	u, err := url.Parse(c.baseURL + c.searchPath)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var sr clawHubSearchResp
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	for _, r := range sr.Results {
		slug := derefStr(r.Slug, "")
		if slug == "" {
			continue
		}
		results = append(results, map[string]interface{}{
			"slug":         slug,
			"display_name": derefStr(r.DisplayName, slug),
			"summary":      derefStr(r.Summary, ""),
			"version":      derefStr(r.Version, ""),
			"score":        r.Score,
		})
	}
	return results, nil
}

func (c *clawHubClient) DownloadAndInstall(ctx context.Context, slug, version, targetDir string) (string, error) {
	u, err := url.Parse(c.baseURL + c.downloadPath)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("slug", slug)
	if version != "" {
		q.Set("version", version)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return "", err
	}
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "ghost-clawhub-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())
	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return "", err
	}
	tmpFile.Close()

	if err := extractZipToDir(tmpFile.Name(), targetDir); err != nil {
		return "", err
	}
	return version, nil
}

func extractZipToDir(zipPath, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}
	cmd := exec.Command("unzip", "-o", zipPath, "-d", targetDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("extract failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// startTime is recorded when the wizard process starts.
var startTime = time.Now()

// ---------- Tools, Gateway & Advanced Configuration ----------

func handleToolsGet(w http.ResponseWriter, r *http.Request) {
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
		"web": map[string]interface{}{
			"brave": map[string]interface{}{
				"enabled":     cfg.Tools.Web.Brave.Enabled,
				"api_key":     maskKey(cfg.Tools.Web.Brave.APIKey),
				"max_results": cfg.Tools.Web.Brave.MaxResults,
			},
			"duckduckgo": map[string]interface{}{
				"enabled":     cfg.Tools.Web.DuckDuckGo.Enabled,
				"max_results": cfg.Tools.Web.DuckDuckGo.MaxResults,
			},
		},
		"curator": map[string]interface{}{
			"enabled":             cfg.Tools.Curator.Enabled,
			"stale_after_days":    cfg.Tools.Curator.StaleAfterDays,
			"archive_after_days":  cfg.Tools.Curator.ArchiveAfterDays,
			"check_interval_mins": cfg.Tools.Curator.CheckIntervalMins,
		},
		"delegation": map[string]interface{}{
			"enabled":        cfg.Tools.Delegation.Enabled,
			"max_concurrent": cfg.Tools.Delegation.MaxConcurrent,
			"max_tasks":      cfg.Tools.Delegation.MaxTasks,
			"budget_tokens":  cfg.Tools.Delegation.BudgetTokens,
		},
	})
}

func handleToolsSet(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Web *struct {
			Brave *struct {
				Enabled    bool   `json:"enabled"`
				APIKey     string `json:"api_key"`
				MaxResults int    `json:"max_results"`
			} `json:"brave"`
			DuckDuckGo *struct {
				Enabled    bool `json:"enabled"`
				MaxResults int  `json:"max_results"`
			} `json:"duckduckgo"`
		} `json:"web"`
		Curator *struct {
			Enabled           bool `json:"enabled"`
			StaleAfterDays    int  `json:"stale_after_days"`
			ArchiveAfterDays  int  `json:"archive_after_days"`
			CheckIntervalMins int  `json:"check_interval_mins"`
		} `json:"curator"`
		Delegation *struct {
			Enabled       bool `json:"enabled"`
			MaxConcurrent int  `json:"max_concurrent"`
			MaxTasks      int  `json:"max_tasks"`
			BudgetTokens  int  `json:"budget_tokens"`
		} `json:"delegation"`
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

	if req.Web != nil {
		if req.Web.Brave != nil {
			cfg.Tools.Web.Brave.Enabled = req.Web.Brave.Enabled
			if req.Web.Brave.APIKey != "" && !strings.HasPrefix(req.Web.Brave.APIKey, "••") {
				cfg.Tools.Web.Brave.APIKey = req.Web.Brave.APIKey
			}
			if req.Web.Brave.MaxResults > 0 {
				cfg.Tools.Web.Brave.MaxResults = req.Web.Brave.MaxResults
			}
		}
		if req.Web.DuckDuckGo != nil {
			cfg.Tools.Web.DuckDuckGo.Enabled = req.Web.DuckDuckGo.Enabled
			if req.Web.DuckDuckGo.MaxResults > 0 {
				cfg.Tools.Web.DuckDuckGo.MaxResults = req.Web.DuckDuckGo.MaxResults
			}
		}
	}
	if req.Curator != nil {
		cfg.Tools.Curator.Enabled = req.Curator.Enabled
		if req.Curator.StaleAfterDays > 0 {
			cfg.Tools.Curator.StaleAfterDays = req.Curator.StaleAfterDays
		}
		if req.Curator.ArchiveAfterDays > 0 {
			cfg.Tools.Curator.ArchiveAfterDays = req.Curator.ArchiveAfterDays
		}
		if req.Curator.CheckIntervalMins > 0 {
			cfg.Tools.Curator.CheckIntervalMins = req.Curator.CheckIntervalMins
		}
	}
	if req.Delegation != nil {
		cfg.Tools.Delegation.Enabled = req.Delegation.Enabled
		if req.Delegation.MaxConcurrent > 0 {
			cfg.Tools.Delegation.MaxConcurrent = req.Delegation.MaxConcurrent
		}
		if req.Delegation.MaxTasks > 0 {
			cfg.Tools.Delegation.MaxTasks = req.Delegation.MaxTasks
		}
		if req.Delegation.BudgetTokens > 0 {
			cfg.Tools.Delegation.BudgetTokens = req.Delegation.BudgetTokens
		}
	}

	if err := config.SaveConfig(fb.ConfigPath, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Tools settings saved"})
}

func handleGatewayGet(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	cfg, err := config.LoadConfig(fb.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":   true,
		"host": cfg.Gateway.Host,
		"port": cfg.Gateway.Port,
	})
}

func handleGatewaySet(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Host string `json:"host"`
		Port int    `json:"port"`
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

	if req.Host != "" {
		cfg.Gateway.Host = req.Host
	}
	if req.Port > 0 && req.Port < 65536 {
		cfg.Gateway.Port = req.Port
	}

	if err := config.SaveConfig(fb.ConfigPath, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Gateway settings saved"})
}

func handleAdvancedGet(w http.ResponseWriter, r *http.Request) {
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
		"rag": map[string]interface{}{
			"enabled":         cfg.RAG.Enabled,
			"m":               cfg.RAG.M,
			"ef_construction": cfg.RAG.EfConstruction,
			"ef_search":       cfg.RAG.EfSearch,
		},
		"nudge": map[string]interface{}{
			"enabled":         cfg.Nudge.Enabled,
			"memory_interval": cfg.Nudge.MemoryInterval,
			"skill_interval":  cfg.Nudge.SkillInterval,
		},
		"devices": map[string]interface{}{
			"enabled":     cfg.Devices.Enabled,
			"monitor_usb": cfg.Devices.MonitorUSB,
		},
		"routing": map[string]interface{}{
			"light_model": cfg.Agents.Routing.LightModel,
			"threshold":   cfg.Agents.Routing.Threshold,
		},
		"search_enabled":        cfg.Agents.Defaults.SearchEnabled,
		"restrict_to_workspace": cfg.Agents.Defaults.RestrictToWorkspace,
		"max_tool_iterations":   cfg.Agents.Defaults.MaxToolIterations,
		"mcp": map[string]interface{}{
			"enabled": cfg.Tools.MCP.Enabled,
			"servers": cfg.Tools.MCP.Servers,
		},
	})
}

func handleAdvancedSet(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RAG *struct {
			Enabled        bool `json:"enabled"`
			M              int  `json:"m"`
			EfConstruction int  `json:"ef_construction"`
			EfSearch       int  `json:"ef_search"`
		} `json:"rag"`
		Nudge *struct {
			Enabled        bool `json:"enabled"`
			MemoryInterval int  `json:"memory_interval"`
			SkillInterval  int  `json:"skill_interval"`
		} `json:"nudge"`
		Devices *struct {
			Enabled    bool `json:"enabled"`
			MonitorUSB bool `json:"monitor_usb"`
		} `json:"devices"`
		Routing *struct {
			LightModel string  `json:"light_model"`
			Threshold  float64 `json:"threshold"`
		} `json:"routing"`
		SearchEnabled       *bool `json:"search_enabled"`
		RestrictToWorkspace *bool `json:"restrict_to_workspace"`
		MaxToolIterations   *int  `json:"max_tool_iterations"`
		MCP                 *struct {
			Enabled bool                              `json:"enabled"`
			Servers map[string]config.MCPServerConfig `json:"servers"`
		} `json:"mcp"`
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

	if req.RAG != nil {
		cfg.RAG.Enabled = req.RAG.Enabled
		if req.RAG.M > 0 {
			cfg.RAG.M = req.RAG.M
		}
		if req.RAG.EfConstruction > 0 {
			cfg.RAG.EfConstruction = req.RAG.EfConstruction
		}
		if req.RAG.EfSearch > 0 {
			cfg.RAG.EfSearch = req.RAG.EfSearch
		}
	}
	if req.Nudge != nil {
		cfg.Nudge.Enabled = req.Nudge.Enabled
		if req.Nudge.MemoryInterval > 0 {
			cfg.Nudge.MemoryInterval = req.Nudge.MemoryInterval
		}
		if req.Nudge.SkillInterval > 0 {
			cfg.Nudge.SkillInterval = req.Nudge.SkillInterval
		}
	}
	if req.Devices != nil {
		cfg.Devices.Enabled = req.Devices.Enabled
		cfg.Devices.MonitorUSB = req.Devices.MonitorUSB
	}
	if req.Routing != nil {
		cfg.Agents.Routing.LightModel = req.Routing.LightModel
		if req.Routing.Threshold > 0 {
			cfg.Agents.Routing.Threshold = req.Routing.Threshold
		}
	}
	if req.SearchEnabled != nil {
		cfg.Agents.Defaults.SearchEnabled = *req.SearchEnabled
	}
	if req.RestrictToWorkspace != nil {
		cfg.Agents.Defaults.RestrictToWorkspace = *req.RestrictToWorkspace
	}
	if req.MaxToolIterations != nil && *req.MaxToolIterations > 0 {
		cfg.Agents.Defaults.MaxToolIterations = *req.MaxToolIterations
	}
	if req.MCP != nil {
		cfg.Tools.MCP.Enabled = req.MCP.Enabled
		if req.MCP.Servers != nil {
			cfg.Tools.MCP.Servers = req.MCP.Servers
		}
	}

	if err := config.SaveConfig(fb.ConfigPath, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Advanced settings saved"})
}

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

// ── Integrations (product setup for Calendar / Flight / Home Assistant) ──
// These reuse the existing admin session + .secrets.json boundary. Secrets
// are masked on read and never logged. Calendar uses gcalcli's device flow
// (no Pi inbound needed); flight/HASS store keys in ProviderAPIKeys.

func handleIntegrationsStatus(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	cal := skills.CalendarCheck()
	flightReady := skills.AviationKey(nil) != ""
	hassReady := skills.HassConfigured()
	camReady := skills.CameraCheck()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true,
		"integrations": map[string]interface{}{
			"calendar": map[string]interface{}{
				"status":     string(cal.Status),
				"connected":  cal.Connected,
				"message":    cal.Message,
				"needsSetup": cal.NeedsSetup,
			},
			"flight": map[string]interface{}{
				"configured": flightReady,
				"status":     map[bool]string{true: "ready", false: "needs_configuration"}[flightReady],
			},
			"homeassistant": map[string]interface{}{
				"configured": hassReady,
				"status":     map[bool]string{true: "ready", false: "needs_configuration"}[hassReady],
			},
			"camera": map[string]interface{}{
				"available": camReady.Available,
				"detail":    camReady.Detail,
				"status":    map[bool]string{true: "ready", false: "needs_configuration"}[camReady.Available],
			},
		},
	})
}

func handleIntegrationsCalendarStart(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// If already connected, report ready (idempotent).
	if st := skills.CalendarCheck(); st.Connected {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "status": "ready", "message": st.Message})
		return
	}
	url, err := skills.CalendarDeviceFlow(30 * time.Second)
	if err != nil {
		if errors.Is(err, skills.ErrCalendarToolMissing) {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"ok": true, "status": "needs_setup",
				"message": "gcalcli isn't available to the Ghost service yet. Install it with `pip install gcalcli` (or symlink it into /usr/local/bin), restart ghost-web, then try again.",
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "could not start calendar setup; try again shortly"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true, "status": "needs_authorization",
		"setup_url": url,
		"message":   "Visit the setup URL on any device, approve Google Calendar access, then poll status until connected.",
	})
}

func handleIntegrationsCalendarDisconnect(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := skills.CalendarDisconnect(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "could not disconnect calendar"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "status": "needs_setup"})
}

func handleIntegrationsFlightSave(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid request"})
		return
	}
	req.APIKey = strings.TrimSpace(req.APIKey)
	if req.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "api_key is required"})
		return
	}
	cfg, err := config.LoadConfig(fb.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	// Product path: ProviderAPIKeys map -> .secrets.json (0600). Never log key.
	secrets, err := config.LoadSecrets(config.SecretsPath(fb.ConfigPath))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	if secrets.ProviderAPIKeys == nil {
		secrets.ProviderAPIKeys = map[string]string{}
	}
	secrets.ProviderAPIKeys["aviationstack"] = req.APIKey
	if err := config.SaveSecrets(config.SecretsPath(fb.ConfigPath), secrets); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	_ = cfg
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "status": "ready"})
}

func handleIntegrationsHassSave(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid request"})
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	req.Token = strings.TrimSpace(req.Token)
	if req.URL == "" || req.Token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "url and token are required"})
		return
	}
	secrets, err := config.LoadSecrets(config.SecretsPath(fb.ConfigPath))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	if secrets.ProviderAPIKeys == nil {
		secrets.ProviderAPIKeys = map[string]string{}
	}
	// Reuse generic map (no new top-level secret fields): namespaced keys.
	secrets.ProviderAPIKeys["hass_url"] = req.URL
	secrets.ProviderAPIKeys["hass_token"] = req.Token
	if err := config.SaveSecrets(config.SecretsPath(fb.ConfigPath), secrets); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "status": "ready"})
}
