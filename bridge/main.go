package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Port          string   `json:"port"`
	BridgeSecret  string   `json:"bridge_secret"`
	AllowedCmds   []string `json:"allowed_cmds"`
	ScreenshotCmd string   `json:"screenshot_cmd"`
}

type ExecRequest struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

type ExecResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Duration int64  `json:"duration_ms"`
}

type OpenRequest struct {
	Target string `json:"target"`
}

var cfg Config
var startupTime = time.Now()

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Ghost-Secret")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		secret := r.Header.Get("X-Ghost-Secret")
		if secret == "" {
			log.Printf("⚠️ Unauthorized access attempt from %s: Secret is missing", r.RemoteAddr)
			http.Error(w, `{"error":"unauthorized: secret missing"}`, http.StatusUnauthorized)
			return
		}
		if secret != cfg.BridgeSecret {
			log.Printf("⚠️ Unauthorized access attempt from %s: Secret mismatch", r.RemoteAddr)
			http.Error(w, `{"error":"unauthorized: secret mismatch"}`, http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func handleExec(w http.ResponseWriter, r *http.Request) {
	var req ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Command == "" {
		http.Error(w, `{"error":"bad request"}`, 400)
		return
	}

	allowed := false
	for _, prefix := range cfg.AllowedCmds {
		if strings.HasPrefix(req.Command, prefix) {
			allowed = true
			break
		}
	}
	safeDefaults := []string{
		"xdg-open ", "systemctl status ", "df ", "free ", "uptime", "hostname",
		"date", "ls ", "cat /proc/", "journalctl -u ghost", "ping -c",
	}
	for _, s := range safeDefaults {
		if strings.HasPrefix(req.Command, s) || req.Command == s {
			allowed = true
			break
		}
	}
	if !allowed {
		http.Error(w, `{"error":"command not in allowlist"}`, 403)
		return
	}

	timeout := req.Timeout
	if timeout <= 0 || timeout > 30 {
		timeout = 10
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", req.Command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	exitCode := 0
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	_ = json.NewEncoder(w).Encode(ExecResponse{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		Duration: time.Since(start).Milliseconds(),
	})
}

func handleScreenshot(w http.ResponseWriter, r *http.Request) {
	outPath := "/tmp/ghost-bridge-screen.png"
	scmdStr := cfg.ScreenshotCmd

	// If no explicit command is set, try to find a suitable tool
	if scmdStr == "" {
		// Detect Wayland vs X11
		isWayland := os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "wayland"

		if isWayland {
			// Try Wayland-native tools first
			if _, err := exec.LookPath("grim"); err == nil {
				scmdStr = "grim " + outPath
			} else if _, err := exec.LookPath("gnome-screenshot"); err == nil {
				scmdStr = "gnome-screenshot -f " + outPath
			}
		}

		// Fallback to X11 tools if not already set or if Wayland check skipped
		if scmdStr == "" {
			for _, tool := range []string{"scrot", "import", "raspi2png"} {
				if _, err := exec.LookPath(tool); err == nil {
					switch tool {
					case "scrot":
						scmdStr = "scrot -z " + outPath // -z for silent
					case "import":
						scmdStr = "import -window root " + outPath
					case "raspi2png":
						scmdStr = "raspi2png -p " + outPath
					}
					break
				}
			}
		}
	}

	if scmdStr == "" {
		http.Error(w, `{"error":"no screenshot tool found (scrot, grim, or raspi2png)"}`, 500)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to capture with common environment variables set
	cmd := exec.CommandContext(ctx, "bash", "-c", scmdStr)

	// Ensure we have basic display environment variables if they are missing
	// This helps when running as a service
	env := os.Environ()
	hasDisplay := false
	hasWayland := false
	for _, e := range env {
		if strings.HasPrefix(e, "DISPLAY=") {
			hasDisplay = true
		}
		if strings.HasPrefix(e, "WAYLAND_DISPLAY=") {
			hasWayland = true
		}
	}

	if !hasDisplay && !hasWayland {
		// Guessing default display for X11 if none set
		cmd.Env = append(env, "DISPLAY=:0")
	} else {
		cmd.Env = env
	}

	if err := cmd.Run(); err != nil {
		// If it failed and we didn't try raspi2png yet, try it as a last resort
		// raspi2png doesn't need X or Wayland, it reads the hardware framebuffer
		if !strings.Contains(scmdStr, "raspi2png") {
			if _, errPath := exec.LookPath("raspi2png"); errPath == nil {
				cmdFallback := exec.CommandContext(ctx, "raspi2png", "-p", outPath)
				if errFallback := cmdFallback.Run(); errFallback == nil {
					goto captureSuccess
				}
			}
		}
		http.Error(w, fmt.Sprintf(`{"error":"screenshot failed: %s"}`, err.Error()), 500)
		return
	}

captureSuccess:
	imgBytes, err := os.ReadFile(outPath)
	if err != nil {
		http.Error(w, `{"error":"could not read screenshot result"}`, 500)
		return
	}
	_ = os.Remove(outPath)

	b64 := base64.StdEncoding.EncodeToString(imgBytes)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"image":     b64,
		"mime_type": "image/png",
	})
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	stats := map[string]string{}
	cmds := map[string]string{
		"uptime":    "uptime -p",
		"cpu_temp":  "vcgencmd measure_temp 2>/dev/null || awk '{printf \"%.1fc\", $1/1000}' /sys/class/thermal/thermal_zone0/temp 2>/dev/null",
		"memory":    "free -h | awk '/^Mem:/ {print $3\"/\"$2}'",
		"disk":      "df -h / | awk 'NR==2 {print $3\"/\"$2\" (\"$5\")\"}'",
		"load":      "cut -d' ' -f1-3 /proc/loadavg",
		"ip":        "hostname -I | awk '{print $1}'",
		"hostname":  "hostname",
		"ghost_svc": "systemctl is-active ghost 2>/dev/null",
	}
	for key, cmdStr := range cmds {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		out, err := exec.CommandContext(ctx, "bash", "-c", cmdStr).Output()
		cancel()
		if err == nil {
			stats[key] = strings.TrimSpace(string(out))
		} else {
			stats[key] = "—"
		}
	}
	stats["timestamp"] = fmt.Sprintf("%d", time.Now().Unix())
	_ = json.NewEncoder(w).Encode(stats)
}

func handleOpen(w http.ResponseWriter, r *http.Request) {
	var req OpenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Target == "" {
		http.Error(w, `{"error":"bad request"}`, 400)
		return
	}
	target := req.Target
	isURL := strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://")

	knownApps := map[string]string{
		"firefox":  "firefox", "chromium": "chromium-browser",
		"chrome":   "chromium-browser", "terminal": "x-terminal-emulator",
		"files":    "xdg-open /home", "spotify": "spotify",
		"vlc":      "vlc", "gedit": "gedit", "calculator": "gnome-calculator",
	}

	var cmdStr string
	if isURL {
		cmdStr = "xdg-open " + shellescape(target)
	} else if appCmd, ok := knownApps[strings.ToLower(target)]; ok {
		cmdStr = appCmd + " &"
	} else {
		http.Error(w, `{"error":"unknown app or invalid URL"}`, 400)
		return
	}

	cmd := exec.Command("bash", "-c", "DISPLAY=:0 "+cmdStr)
	cmd.Env = append(os.Environ(), "DISPLAY=:0")
	err := cmd.Start()
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "launched": target})
}

func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
		"version":   "2.0.0",
		"uptime_s":  int64(time.Since(startupTime).Seconds()),
	})
}

func main() {
	home := os.Getenv("HOME")
	envCandidates := []string{
		os.Getenv("ENV_FILE"),
		".env",
		"../.env",
		filepath.Join(home, "ghost", ".env"),
		filepath.Join(home, ".env"),
	}
	for _, candidate := range envCandidates {
		if candidate == "" {
			continue
		}
		if _, statErr := os.Stat(candidate); statErr == nil {
			log.Printf("🔍 Loading env from: %s", candidate)
			loadDotEnv(candidate)
			break
		}
	}

	cfg = Config{
		Port:          getEnv("BRIDGE_PORT", "8766"), // Default to 8766 for Remote Control
		BridgeSecret:  getEnv("BRIDGE_SECRET", ""),
		ScreenshotCmd: getEnv("SCREENSHOT_CMD", ""),
	}

	if cfg.BridgeSecret == "" {
		log.Println("⚠️  WARNING: BRIDGE_SECRET is not set. Using default 'ghost-pi-secret'.")
		cfg.BridgeSecret = "ghost-pi-secret"
	}

	if raw := getEnv("ALLOWED_CMDS", ""); raw != "" {
		cfg.AllowedCmds = strings.Split(raw, ",")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", authMiddleware(handleHealth))
	mux.HandleFunc("/v1/exec", authMiddleware(handleExec))
	mux.HandleFunc("/v1/screenshot", authMiddleware(handleScreenshot))
	mux.HandleFunc("/v1/stats", authMiddleware(handleStats))
	mux.HandleFunc("/v1/open", authMiddleware(handleOpen))
	mux.HandleFunc("/health", authMiddleware(handleHealth))
	mux.HandleFunc("/exec", authMiddleware(handleExec))
	mux.HandleFunc("/screenshot", authMiddleware(handleScreenshot))
	mux.HandleFunc("/stats", authMiddleware(handleStats))
	mux.HandleFunc("/open", authMiddleware(handleOpen))

	addr := "0.0.0.0:" + cfg.Port
	log.Printf("🔧 Ghost Remote Bridge running on %s (remote control only)", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func loadDotEnv(envPath string) {
	data, err := os.ReadFile(envPath)
	if err != nil {
		return
	}
	loaded := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if os.Getenv(key) == "" && val != "" {
			_ = os.Setenv(key, val)
			loaded++
		}
	}
	if loaded > 0 {
		log.Printf("📄 Loaded %d vars from %s", loaded, envPath)
	} else {
		log.Printf("📄 Found %s but all keys already set in environment", envPath)
	}
}
