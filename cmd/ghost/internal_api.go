// Ghost Internal API — lightweight HTTP server for bridge-to-agent routing
// Exposes ProcessDirectWithChannel() via HTTP so the mobile app can talk directly
// to the full Ghost agent runtime (tools, RAG, memory, skills).
//
// Listens on 0.0.0.0 — accessible to local network devices (e.g., phone).
// Protected by BRIDGE_SECRET header matching.

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/cron"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/tools"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var apiStartTime = time.Now()

func handleWebSocket(agentLoop *agent.AgentLoop) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secret := os.Getenv("BRIDGE_SECRET")
		if secret != "" {
			got := r.URL.Query().Get("secret")
			if got == "" {
				got = r.Header.Get("X-Ghost-Secret")
			}
			if got == "" {
				got = r.Header.Get("Authorization")
			}
			if got != secret && got != "Bearer "+secret {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("❌ Failed to upgrade websocket: %v", err)
			return
		}
		defer conn.Close()

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		outboundCh, unsubscribe := agentLoop.Bus().SubscribeOutbound("mobile-ws", false, 300)
		defer unsubscribe()

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-outboundCh:
				if !ok {
					return
				}
				sessionID, _ := msg.Metadata["session_id"].(string)
				messageID, _ := msg.Metadata["message_id"].(string)
				logger.DebugCF("internal-api", "WS outbound dequeued", map[string]interface{}{
					"channel":    msg.Channel,
					"chat_id":    msg.ChatID,
					"session_id": sessionID,
					"message_id": messageID,
				})

				// Only forward mobile-channel messages and canvas updates to the app.
				// Telegram and CLI responses must not appear in the mobile chat.
				if msg.Channel != "mobile" {
					meta, _ := msg.Metadata["type"].(string)
					if meta != "canvas_update" && meta != "cron_update" {
						continue // skip — wrong channel
					}
				}

				payload := map[string]interface{}{
					"channel":  msg.Channel,
					"chat_id":  msg.ChatID,
					"content":  msg.Content,
					"metadata": msg.Metadata,
				}
				if t, ok := msg.Metadata["type"].(string); ok && t != "" {
					payload["type"] = t
				}
				if id, ok := msg.Metadata["message_id"].(string); ok && id != "" {
					payload["id"] = id
				}
				if sid, ok := msg.Metadata["session_id"].(string); ok && sid != "" {
					payload["session_id"] = sid
				}
				switch ts := msg.Metadata["timestamp"].(type) {
				case int64:
					payload["timestamp"] = ts
				case int:
					payload["timestamp"] = int64(ts)
				case float64:
					payload["timestamp"] = int64(ts)
				}

				_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if err := conn.WriteJSON(payload); err != nil {
					log.Printf("❌ WebSocket write error: %v", err)
					return
				}
			}
		}
	}
}

func enrichWeatherPrompt(content string, metadata map[string]string) string {
	lc := strings.ToLower(content)
	if !strings.Contains(lc, "weather") && !strings.Contains(lc, "forecast") && !strings.Contains(lc, "temperature") {
		return content
	}
	if strings.Contains(lc, " in ") || strings.Contains(lc, " at ") || strings.Contains(lc, " for ") {
		return content
	}
	city := strings.TrimSpace(metadata["city"])
	region := strings.TrimSpace(metadata["region"])
	country := strings.TrimSpace(metadata["country"])
	lat := strings.TrimSpace(metadata["latitude"])
	lon := strings.TrimSpace(metadata["longitude"])
	tz := strings.TrimSpace(metadata["timezone"])
	source := strings.TrimSpace(metadata["location_source"])
	details := []string{}
	if city != "" {
		details = append(details, "city="+city)
	}
	if region != "" {
		details = append(details, "region="+region)
	}
	if country != "" {
		details = append(details, "country="+country)
	}
	if lat != "" && lon != "" {
		details = append(details, "lat="+lat)
		details = append(details, "lon="+lon)
	}
	if tz != "" {
		details = append(details, "timezone="+tz)
	}
	if source != "" {
		details = append(details, "source="+source)
	}
	if len(details) == 0 {
		return content + "\n\nIf location is unknown, ask a short clarification or clearly label fallback location source."
	}
	return content + "\n\nUser weather location context: " + strings.Join(details, ", ") + ". Use this location unless user explicitly asked another place. If falling back, state the fallback source explicitly."
}

const defaultInternalAPIPort = 8766

func authMiddleware(secret string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Ghost-Secret, X-Ghost-Session")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if secret != "" {
			got := r.Header.Get("X-Ghost-Secret")
			if got == "" {
				got = r.Header.Get("Authorization")
			}
			if got != secret && got != "Bearer "+secret {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		next(w, r)
	}
}

func jsonResponse(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func jsonError(w http.ResponseWriter, status int, kind, message string) {
	jsonResponse(w, status, map[string]interface{}{
		"error": map[string]string{
			"kind":    kind,
			"message": message,
		},
	})
}

func resolveSession(r *http.Request) string {
	if s := r.Header.Get("X-Ghost-Session"); s != "" {
		return s
	}
	if s := r.URL.Query().Get("session"); s != "" {
		return s
	}
	return "mobile:default"
}

// toolStatusLabel returns a human-readable label for a tool call.
// Sent to the mobile app as a tool_status SSE event so the user can see
// exactly what Ghost is doing during long multi-step operations.
func toolStatusLabel(name, args string) string {
	var a map[string]interface{}
	_ = json.Unmarshal([]byte(args), &a)

	switch name {
	case "web_search":
		if q, ok := a["query"].(string); ok && q != "" {
			if len(q) > 40 {
				q = q[:40] + "…"
			}
			return "Searching: " + q
		}
		return "Searching the web…"
	case "web_fetch":
		if u, ok := a["url"].(string); ok && u != "" {
			if len(u) > 45 {
				u = u[:45] + "…"
			}
			return "Fetching: " + u
		}
		return "Fetching page…"
	case "exec":
		if c, ok := a["command"].(string); ok && c != "" {
			if len(c) > 40 {
				c = c[:40] + "…"
			}
			return "Running: " + c
		}
		return "Running shell command…"
	case "sandbox":
		if c, ok := a["command"].(string); ok && c != "" {
			if len(c) > 40 {
				c = c[:40] + "…"
			}
			return "Sandbox: " + c
		}
		return "Running in sandbox…"
	case "read_file":
		if p, ok := a["path"].(string); ok && p != "" {
			parts := strings.Split(p, "/")
			return "Reading: " + parts[len(parts)-1]
		}
		return "Reading file…"
	case "write_file":
		if p, ok := a["path"].(string); ok && p != "" {
			parts := strings.Split(p, "/")
			return "Writing: " + parts[len(parts)-1]
		}
		return "Writing file…"
	case "list_dir":
		if p, ok := a["path"].(string); ok && p != "" {
			parts := strings.Split(strings.TrimRight(p, "/"), "/")
			return "Listing: " + parts[len(parts)-1] + "/"
		}
		return "Listing directory…"
	case "browser":
		if u, ok := a["url"].(string); ok && u != "" {
			if len(u) > 40 {
				u = u[:40] + "…"
			}
			return "Browser: " + u
		}
		return "Opening browser…"
	case "canvas":
		return "Rendering canvas…"
	case "oracle":
		return "Loading context…"
	case "remember":
		return "Saving to memory…"
	case "spawn", "subagent":
		return "Spawning subagent…"
	case "screenshot":
		return "Capturing screenshot…"
	case "edit_file":
		if p, ok := a["path"].(string); ok && p != "" {
			parts := strings.Split(p, "/")
			return "Editing: " + parts[len(parts)-1]
		}
		return "Editing file…"
	default:
		return "Using " + name + "…"
	}
}

type internalAPIRequest struct {
	Content    string            `json:"content"`
	SessionKey string            `json:"session_key"`
	Media      []string          `json:"media,omitempty"`
	MediaItems []MediaItem       `json:"media_items,omitempty"`
	Channel    string            `json:"channel,omitempty"`
	ChatID     string            `json:"chat_id,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type MediaItem struct {
	Base64   string `json:"base64"`
	MimeType string `json:"mime_type,omitempty"`
	Filename string `json:"filename,omitempty"`
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

type Message struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
	MediaType string `json:"media_type,omitempty"`
	MediaURL  string `json:"media_url,omitempty"`
}

type HistoryResponse struct {
	Messages []Message `json:"messages"`
	Total    int       `json:"total"`
}

type SearchResult struct {
	ID        string  `json:"id"`
	SessionID string  `json:"session_id"`
	Role      string  `json:"role"`
	Content   string  `json:"content"`
	Timestamp int64   `json:"timestamp"`
	Rank      float64 `json:"rank"`
}

type DoctorResponse struct {
	Status    string               `json:"status"`
	Checks    []DoctorCheckPayload `json:"checks"`
	Timestamp int64                `json:"timestamp"`
	Uptime    int64                `json:"uptime"`
	Version   string               `json:"version"`
	Profile   ProfileInfo          `json:"profile"`
}

type ProfileInfo struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

type DoctorCheckPayload struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

type CronStateResponse struct {
	ID        string     `json:"id"`
	State     string     `json:"state"`
	PausedAt  *time.Time `json:"paused_at,omitempty"`
	ResumedAt *time.Time `json:"resumed_at,omitempty"`
	NextRunAt *time.Time `json:"next_run_at,omitempty"`
}

type CronTriggerResponse struct {
	ID          string    `json:"id"`
	Triggered   bool      `json:"triggered"`
	RunAsync    bool      `json:"run_async"`
	TriggeredAt time.Time `json:"triggered_at"`
}

type cronPatchRequestBody struct {
	ID      string         `json:"id"`
	Updates cron.JobUpdate `json:"updates"`
}

func resolveRequestChannel(existing, clientType, userAgent string) string {
	if strings.TrimSpace(existing) != "" {
		return existing
	}
	if strings.EqualFold(strings.TrimSpace(clientType), "mobile") || strings.Contains(strings.ToLower(userAgent), "expo") {
		return "mobile"
	}
	return "cli"
}

func decodeCronPatchRequest(r *http.Request, pathID string) (string, cron.JobUpdate, error) {
	var body cronPatchRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return "", cron.JobUpdate{}, fmt.Errorf("invalid request: %w", err)
	}
	id := strings.TrimSpace(pathID)
	if id == "" {
		id = strings.TrimSpace(body.ID)
	}
	if id == "" {
		return "", cron.JobUpdate{}, fmt.Errorf("id is required")
	}
	return id, body.Updates, nil
}

func buildCronStateResponse(id string, state string, pausedAt, resumedAt, nextRunAt *time.Time) CronStateResponse {
	return CronStateResponse{
		ID:        id,
		State:     state,
		PausedAt:  pausedAt,
		ResumedAt: resumedAt,
		NextRunAt: nextRunAt,
	}
}

func buildCronTriggerResponse(id string, now time.Time) CronTriggerResponse {
	return CronTriggerResponse{
		ID:          id,
		Triggered:   true,
		RunAsync:    true,
		TriggeredAt: now,
	}
}

func handleExec(allowedCmds []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ExecRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Command == "" {
			http.Error(w, `{"error":"bad request"}`, 400)
			return
		}

		allowed := false
		for _, prefix := range allowedCmds {
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
}

func handleScreenshot(screenshotCmd string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		outPath := "/tmp/ghost-bridge-screen.png"
		scmdStr := screenshotCmd

		if scmdStr == "" {
			isWayland := os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "wayland"

			if isWayland {
				if _, err := exec.LookPath("grim"); err == nil {
					scmdStr = "grim " + outPath
				} else if _, err := exec.LookPath("gnome-screenshot"); err == nil {
					scmdStr = "gnome-screenshot -f " + outPath
				}
			}

			if scmdStr == "" {
				for _, tool := range []string{"scrot", "import", "raspi2png"} {
					if _, err := exec.LookPath(tool); err == nil {
						switch tool {
						case "scrot":
							scmdStr = "scrot -z " + outPath
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

		cmd := exec.CommandContext(ctx, "bash", "-c", scmdStr)

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
			cmd.Env = append(env, "DISPLAY=:0")
		} else {
			cmd.Env = env
		}

		if err := cmd.Run(); err != nil {
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
		"firefox":    "firefox",
		"chromium":   "chromium-browser",
		"chrome":     "chromium-browser",
		"terminal":   "x-terminal-emulator",
		"files":      "xdg-open /home",
		"spotify":    "spotify",
		"vlc":        "vlc",
		"gedit":      "gedit",
		"calculator": "gnome-calculator",
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

// startInternalAPI starts the Ghost API server.
// Listens on 0.0.0.0 so the mobile app can connect directly over Wi-Fi.
// One port (GHOST_API_PORT, default 8766) handles everything:
// chat, history, memory, transcription, remote control, and WebSocket.
func startInternalAPI(agentLoop *agent.AgentLoop, cronService *cron.CronService) {
	port := defaultInternalAPIPort
	if p := os.Getenv("GHOST_API_PORT"); p != "" {
		fmt.Sscanf(p, "%d", &port)
	} else if p := os.Getenv("GHOST_INTERNAL_API_PORT"); p != "" {
		fmt.Sscanf(p, "%d", &port)
	}

	secret := os.Getenv("BRIDGE_SECRET")
	allowedCmds := []string{}
	if raw := os.Getenv("ALLOWED_CMDS"); raw != "" {
		allowedCmds = strings.Split(raw, ",")
	}
	screenshotCmd := os.Getenv("SCREENSHOT_CMD")

	memoryDir := os.Getenv("MEMORY_DIR")
	if memoryDir == "" {
		home := os.Getenv("HOME")
		memoryDir = filepath.Join(home, "ghost", "workspace", "memory")
	}

	db := agentLoop.DB()

	mux := http.NewServeMux()

	// ── 1. Health ─────────────────────────────────────────────────────────
	mux.HandleFunc("/v1/health", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "ok",
			"timestamp": time.Now().Unix(),
			"version":   "2.0.0",
			"uptime_s":  int64(time.Since(apiStartTime).Seconds()),
		})
	}))

	mux.HandleFunc("/v1/doctor", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		doctorRunner := agentLoop.Doctor()
		if doctorRunner == nil {
			http.Error(w, `{"error":"doctor unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		results := doctorRunner.RunAll(r.Context())
		overall := "ok"
		checks := make([]DoctorCheckPayload, 0, len(results))
		for _, check := range results {
			checks = append(checks, DoctorCheckPayload{
				Name:      check.Name,
				Status:    check.Status,
				Message:   check.Message,
				LatencyMS: check.Latency,
			})
			if check.Status == "error" {
				overall = "error"
				continue
			}
			if check.Status == "warning" && overall != "error" {
				overall = "warning"
			}
		}

		profileName := agentLoop.GetToolProfile()
		permissions := tools.ProfileAllowlists[profileName]
		if permissions == nil {
			permissions = []string{"*"} // Full access
		}

		_ = json.NewEncoder(w).Encode(DoctorResponse{
			Status:    overall,
			Checks:    checks,
			Timestamp: time.Now().Unix(),
			Uptime:    int64(time.Since(apiStartTime).Seconds()),
			Version:   "2.0.0",
			Profile: ProfileInfo{
				Name:        string(profileName),
				Permissions: permissions,
			},
		})
	}))

	// ── 1b. Cron lifecycle ───────────────────────────────────────────────
	mux.HandleFunc("/v1/cron/jobs/", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		if cronService == nil {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "cron service unavailable")
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/v1/cron/jobs/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) < 1 || parts[0] == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "invalid cron job path")
			return
		}

		jobID := parts[0]
		action := ""
		if len(parts) > 1 {
			action = parts[1]
		}
		if action == "" && r.Method == http.MethodPatch {
			id, updates, err := decodeCronPatchRequest(r, jobID)
			if err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			if updates.Target != nil {
				target := strings.ToLower(strings.TrimSpace(*updates.Target))
				if target != "origin" && target != "local" {
					jsonError(w, http.StatusBadRequest, "invalid_request", "target must be origin or local")
					return
				}
			}
			if err := cronService.UpdateJob(id, updates); err != nil {
				jsonError(w, http.StatusNotFound, "not_found", err.Error())
				return
			}
			status, err := cronService.GetJobStatus(id)
			if err != nil {
				jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "job": status})
			return
		}
		if action == "" {
			jsonError(w, http.StatusBadRequest, "invalid_action", "unsupported cron action")
			return
		}

		switch action {
		case "pause":
			if r.Method != http.MethodPost {
				jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
				return
			}
			if err := cronService.PauseJob(jobID); err != nil {
				jsonError(w, http.StatusNotFound, "not_found", err.Error())
				return
			}
			if job, ok := cronService.GetJob(jobID); ok {
				_ = json.NewEncoder(w).Encode(buildCronStateResponse(job.ID, string(job.LifecycleState), job.PausedAt, nil, job.NextRunAt))
				return
			}
			_ = json.NewEncoder(w).Encode(buildCronStateResponse(jobID, "paused", nil, nil, nil))
		case "resume":
			if r.Method != http.MethodPost {
				jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
				return
			}
			if err := cronService.ResumeJob(jobID); err != nil {
				jsonError(w, http.StatusNotFound, "not_found", err.Error())
				return
			}
			resumedAt := time.Now().UTC()
			if job, ok := cronService.GetJob(jobID); ok {
				_ = json.NewEncoder(w).Encode(buildCronStateResponse(job.ID, string(job.LifecycleState), nil, &resumedAt, job.NextRunAt))
				return
			}
			_ = json.NewEncoder(w).Encode(buildCronStateResponse(jobID, "active", nil, &resumedAt, nil))
		case "run":
			if r.Method != http.MethodPost {
				jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
				return
			}
			if err := cronService.RunJobNow(jobID); err != nil {
				jsonError(w, http.StatusNotFound, "not_found", err.Error())
				return
			}
			_ = json.NewEncoder(w).Encode(buildCronTriggerResponse(jobID, time.Now().UTC()))
		default:
			jsonError(w, http.StatusBadRequest, "invalid_action", "unsupported cron action")
		}
	}))

	mux.HandleFunc("/v1/cron/jobs", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		if cronService == nil {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "cron service unavailable")
			return
		}

		if r.Method == http.MethodGet {
			jobs := cronService.ListJobs(true)
			jsonResponse(w, http.StatusOK, map[string]interface{}{
				"jobs": jobs,
			})
			return
		}

		if r.Method != http.MethodPatch {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}

		id, updates, err := decodeCronPatchRequest(r, "")
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if updates.Target != nil {
			target := strings.ToLower(strings.TrimSpace(*updates.Target))
			if target != "origin" && target != "local" {
				jsonError(w, http.StatusBadRequest, "invalid_request", "target must be origin or local")
				return
			}
		}
		if err := cronService.UpdateJob(id, updates); err != nil {
			jsonError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		status, err := cronService.GetJobStatus(id)
		if err != nil {
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "job": status})
	}))

	// ── 2. Chat (streaming SSE) ───────────────────────────────────────────
	mux.HandleFunc("/v1/chat", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		var req internalAPIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid request: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		if req.Content == "" {
			http.Error(w, `{"error":"content is required"}`, http.StatusBadRequest)
			return
		}

		if req.SessionKey == "" {
			req.SessionKey = resolveSession(r)
		}
		clientType := strings.TrimSpace(r.Header.Get("X-Client-Type"))
		req.Channel = resolveRequestChannel(req.Channel, clientType, r.UserAgent())
		if req.ChatID == "" {
			req.ChatID = "default"
		}

		logger.InfoCF("internal-api", "Processing chat request", map[string]interface{}{
			"session_key":    req.SessionKey,
			"channel":        req.Channel,
			"content_length": len(req.Content),
			"has_media":      len(req.Media) > 0 || len(req.MediaItems) > 0,
		})

		// Prepare SSE
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}

		// Media handling
		mediaPaths := []string{}
		for _, m := range req.Media {
			if tmp, err := saveBase64ToTemp(m); err == nil {
				mediaPaths = append(mediaPaths, tmp)
			}
		}
		for _, item := range req.MediaItems {
			if tmp, err := saveBase64ToTemp(item.Base64); err == nil {
				mediaPaths = append(mediaPaths, tmp)
			}
		}

		// onChunk — streams text tokens to the mobile app as JSON-encoded strings
		onChunk := func(chunk string) {
			escaped, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", string(escaped))
			flusher.Flush()
		}

		// onToolCall — sends a tool_status event so the app can show
		// "Ghost is searching…", "Reading file…" etc. in real time.
		// These are JSON objects, NOT text chunks — the app routes them
		// to the status badge, not the message bubble.
		onToolCall := func(name string, args string) {
			label := toolStatusLabel(name, args)
			payload, _ := json.Marshal(map[string]string{
				"type":  "tool_status",
				"tool":  name,
				"label": label,
			})
			fmt.Fprintf(w, "data: %s\n\n", string(payload))
			flusher.Flush()
		}

		// Keep-alive ticker — prevents the mobile HTTP client from timing out
		// during long tool chains (web fetches, sandbox runs, multi-step reasoning).
		// Sends an SSE comment every 15 seconds; the app silently ignores these.
		keepAliveDone := make(chan struct{})
		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					fmt.Fprintf(w, ": keepalive\n\n")
					flusher.Flush()
				case <-keepAliveDone:
					return
				}
			}
		}()

		ctx := r.Context()
		response, err := agentLoop.ProcessDirectWithChannel(
			ctx,
			enrichWeatherPrompt(req.Content, req.Metadata),
			req.SessionKey,
			req.Channel,
			req.ChatID,
			mediaPaths,
			onChunk,
			onToolCall,
		)

		// Stop keep-alive before writing [DONE] to avoid write-after-close race
		close(keepAliveDone)

		if err != nil {
			logger.ErrorCF("internal-api", "Error processing chat", map[string]interface{}{"error": err.Error()})

			// Clean up any empty/partial assistant message that may have been
			// written to the session before the error occurred.
			if db != nil {
				db.Exec(`
					DELETE FROM messages 
					WHERE session_id = ? 
					  AND role = 'assistant' 
					  AND (content = '' OR content IS NULL)
					  AND created_at > datetime('now', '-2 minutes')
				`, req.SessionKey)
			}

			escaped, _ := json.Marshal("Error: " + err.Error())
			fmt.Fprintf(w, "data: %s\n\n", string(escaped))
			flusher.Flush()
		} else {
			meta := map[string]interface{}{
				"type": "assistant_message",
			}
			if db != nil {
				var messageID string
				var timestamp int64
				err := db.QueryRow(`
					SELECT id, COALESCE(unixepoch(created_at), 0)
					FROM messages
					WHERE session_id = ? AND role = 'assistant'
					ORDER BY datetime(created_at) DESC, rowid DESC
					LIMIT 1
				`, req.SessionKey).Scan(&messageID, &timestamp)
				if err == nil && messageID != "" {
					meta["message_id"] = messageID
					meta["session_id"] = req.SessionKey
					meta["timestamp"] = timestamp
				}
			}
			agentLoop.Bus().PublishOutbound(bus.OutboundMessage{
				Channel:  req.Channel,
				ChatID:   req.ChatID,
				Content:  response,
				Metadata: meta,
			})
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
		}
	}))

	// ── 3. History ────────────────────────────────────────────────────────
	mux.HandleFunc("/v1/history", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		session := resolveSession(r)
		limit := 50
		offset := 0
		since := int64(0)
		fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
		fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
		if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
			if ts, err := strconv.ParseInt(raw, 10, 64); err == nil && ts > 0 {
				since = ts
			}
		}

		if db == nil {
			http.Error(w, `{"error":"database not available"}`, http.StatusInternalServerError)
			return
		}

		var rows *sql.Rows
		var err error
		if since > 0 {
			rows, err = db.Query(`
				SELECT id, role, content, created_at, meta
				FROM messages
				WHERE session_id = ? 
				  AND (archived IS NULL OR archived = 0)
				  AND content IS NOT NULL
				  AND TRIM(content) != ''
				  AND LENGTH(content) > 0
				  AND unixepoch(created_at) > ?
				ORDER BY datetime(created_at) ASC, rowid ASC
				LIMIT ?`, session, since, limit)
		} else {
			rows, err = db.Query(`
				SELECT id, role, content, created_at, meta
				FROM messages
				WHERE session_id = ? 
				  AND (archived IS NULL OR archived = 0)
				  AND content IS NOT NULL
				  AND TRIM(content) != ''
				  AND LENGTH(content) > 0
				ORDER BY datetime(created_at) DESC, rowid DESC
				LIMIT ? OFFSET ?`, session, limit, offset)
		}
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"db error: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var messages []Message
		for rows.Next() {
			var m Message
			var createdAt time.Time
			var metaJSON []byte
			if err := rows.Scan(&m.ID, &m.Role, &m.Content, &createdAt, &metaJSON); err != nil {
				continue
			}
			m.Timestamp = createdAt.Unix()
			messages = append(messages, m)
		}

		if since <= 0 {
			for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
				messages[i], messages[j] = messages[j], messages[i]
			}
		}
		if messages == nil {
			messages = []Message{}
		}

		total := len(messages)
		_ = db.QueryRow(`
			SELECT COUNT(*) FROM messages
			WHERE session_id = ?
			  AND (archived IS NULL OR archived = 0)
			  AND content IS NOT NULL
			  AND TRIM(content) != ''
			  AND LENGTH(content) > 0`, session).Scan(&total)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"messages": messages,
			"total":    total,
		})
	}))

	// ── 4. Search ─────────────────────────────────────────────────────────
	mux.HandleFunc("/v1/search", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		q := r.URL.Query().Get("q")
		if q == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "missing query")
			return
		}
		if db == nil {
			jsonError(w, http.StatusInternalServerError, "internal_error", "database not available")
			return
		}

		limit := 20
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}

		var session string
		if _, hasSession := r.URL.Query()["session"]; hasSession {
			session = strings.TrimSpace(r.URL.Query().Get("session"))
		} else {
			session = resolveSession(r)
		}

		rows, err := db.Query(`
			SELECT
				m.id,
				m.session_id,
				m.role,
				snippet(messages_fts, 0, '[', ']', '…', 32) AS content,
				COALESCE(unixepoch(m.created_at), 0) AS ts,
				bm25(messages_fts) AS rank
			FROM messages_fts
			JOIN messages m ON m.rowid = messages_fts.rowid
			WHERE messages_fts MATCH ?
			  AND (m.archived IS NULL OR m.archived = 0)
			  AND (? = '' OR m.session_id = ?)
			ORDER BY rank
			LIMIT ?`, q, session, session, limit)
		if err != nil {
			rows, err = db.Query(`
				SELECT
					id,
					session_id,
					role,
					content,
					COALESCE(unixepoch(created_at), 0) AS ts,
					0.0 AS rank
				FROM messages
				WHERE content LIKE ?
				  AND (archived IS NULL OR archived = 0)
				  AND (? = '' OR session_id = ?)
				ORDER BY datetime(created_at) DESC
				LIMIT ?`, "%"+q+"%", session, session, limit)
		}
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var results []SearchResult
		for rows.Next() {
			var item SearchResult
			if err := rows.Scan(&item.ID, &item.SessionID, &item.Role, &item.Content, &item.Timestamp, &item.Rank); err != nil {
				continue
			}
			results = append(results, item)
		}
		if results == nil {
			results = []SearchResult{}
		}
		jsonResponse(w, http.StatusOK, results)
	}))

	mux.HandleFunc("/v1/tools", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		type toolEntry struct {
			Name string `json:"name"`
		}
		info := agentLoop.GetStartupInfo()
		rawTools, _ := info["tools"].([]string)
		toolsList := make([]toolEntry, 0, len(rawTools))
		for _, name := range rawTools {
			toolsList = append(toolsList, toolEntry{Name: name})
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"tools": toolsList,
		})
	}))

	// ── 5. Memory files ───────────────────────────────────────────────────
	mux.HandleFunc("/v1/memory/files", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		type FileInfo struct {
			Name     string `json:"name"`
			Modified int64  `json:"modified"`
			Size     int64  `json:"size"`
		}
		var files []FileInfo

		if _, err := os.Stat(memoryDir); err != nil {
			json.NewEncoder(w).Encode([]FileInfo{})
			return
		}

		filepath.Walk(memoryDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(info.Name(), ".md") {
				rel, _ := filepath.Rel(memoryDir, path)
				files = append(files, FileInfo{
					Name:     rel,
					Modified: info.ModTime().Unix(),
					Size:     info.Size(),
				})
			}
			return nil
		})
		if files == nil {
			files = []FileInfo{}
		}
		json.NewEncoder(w).Encode(files)
	}))

	// ── 6. Memory file content ────────────────────────────────────────────
	mux.HandleFunc("/v1/memory/file", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "name required")
			return
		}
		clean := filepath.Clean(name)
		if strings.Contains(clean, "..") || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "\\") {
			jsonError(w, http.StatusForbidden, "forbidden", "invalid path")
			return
		}
		content, err := os.ReadFile(filepath.Join(memoryDir, clean))
		if err != nil {
			jsonError(w, http.StatusNotFound, "not_found", "file not found")
			return
		}
		jsonResponse(w, http.StatusOK, map[string]string{"content": string(content)})
	}))

	// ── 7. Transcribe ─────────────────────────────────────────────────────
	mux.HandleFunc("/v1/transcribe", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		file, _, err := r.FormFile("audio")
		if err != nil {
			http.Error(w, "audio field required", 400)
			return
		}
		defer file.Close()

		apiKey := os.Getenv("KIMI_API_KEY")
		if apiKey == "" {
			http.Error(w, "KIMI_API_KEY not set", 500)
			return
		}

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "audio.webm")
		io.Copy(part, file)
		writer.WriteField("model", "moonshot-v1-auto")
		writer.Close()

		req, _ := http.NewRequest("POST", "https://api.moonshot.cn/v1/audio/transcriptions", body)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", writer.FormDataContentType())

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			http.Error(w, "upstream error", 502)
			return
		}
		defer resp.Body.Close()

		respBytes, _ := io.ReadAll(resp.Body)
		var parsed map[string]interface{}
		if json.Unmarshal(respBytes, &parsed) == nil {
			if text, ok := parsed["text"].(string); ok {
				json.NewEncoder(w).Encode(map[string]string{"text": text})
				return
			}
		}
		http.Error(w, "upstream error", 502)
	}))

	// ── 8. Upload ─────────────────────────────────────────────────────────
	mux.HandleFunc("/v1/upload", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "file field required", 400)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		b64 := base64.StdEncoding.EncodeToString(data)
		json.NewEncoder(w).Encode(map[string]string{
			"b64":       b64,
			"mime_type": header.Header.Get("Content-Type"),
			"filename":  header.Filename,
		})
	}))

	// ── 9. Delete message ─────────────────────────────────────────────────
	mux.HandleFunc("/v1/message", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", 405)
			return
		}
		id := r.URL.Query().Get("id")
		session := resolveSession(r)
		if db != nil && id != "" {
			db.Exec("UPDATE messages SET archived = 1 WHERE id = ? AND session_id = ?", id, session)
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))

	// ── 10. Clear session ─────────────────────────────────────────────────
	mux.HandleFunc("/v1/messages", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", 405)
			return
		}
		session := resolveSession(r)
		if db != nil {
			db.Exec("UPDATE messages SET archived = 1 WHERE session_id = ?", session)
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))

	// ── Remote control endpoints ──────────────────────────────────────────
	mux.HandleFunc("/v1/exec", authMiddleware(secret, handleExec(allowedCmds)))
	mux.HandleFunc("/v1/screenshot", authMiddleware(secret, handleScreenshot(screenshotCmd)))
	mux.HandleFunc("/v1/stats", authMiddleware(secret, handleStats))
	mux.HandleFunc("/v1/open", authMiddleware(secret, handleOpen))

	// ── WebSocket ─────────────────────────────────────────────────────────
	mux.HandleFunc("/v1/ws", handleWebSocket(agentLoop))

	addr := fmt.Sprintf("0.0.0.0:%d", port)
	log.Printf("🤖 Ghost Internal API listening on %s (chat + tools)", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("❌ Internal API failed: %v", err)
	}
}

func saveBase64ToTemp(b64 string) (string, error) {
	// Strip data URL prefix if present (e.g., data:image/png;base64,)
	if idx := strings.Index(b64, ","); idx != -1 {
		b64 = b64[idx+1:]
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp("", "ghost-media-*.bin")
	if err != nil {
		return "", err
	}
	defer tmp.Close()
	tmp.Write(data)
	return tmp.Name(), nil
}
