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
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/logger"
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

		for {
			msg, ok := agentLoop.Bus().SubscribeOutbound(ctx)
			if !ok {
				break
			}
			if err := conn.WriteJSON(msg); err != nil {
				log.Printf("❌ WebSocket write error: %v", err)
				break
			}
		}
	}
}

const defaultInternalAPIPort = 8766

func authMiddleware(secret string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Ghost-Secret, X-Ghost-Session")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
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

func resolveSession(r *http.Request) string {
	if s := r.Header.Get("X-Ghost-Session"); s != "" {
		return s
	}
	if s := r.URL.Query().Get("session"); s != "" {
		return s
	}
	return "mobile:default"
}

type internalAPIRequest struct {
	Content    string       `json:"content"`
	SessionKey string       `json:"session_key"`
	Media      []string     `json:"media,omitempty"` // Legacy: just b64 strings
	MediaItems []MediaItem  `json:"media_items,omitempty"`
	Channel    string       `json:"channel,omitempty"`
	ChatID     string       `json:"chat_id,omitempty"`
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

// startInternalAPI starts the internal API server for bridge-to-agent routing.
// It listens on 0.0.0.0 and is protected by BRIDGE_SECRET.
func startInternalAPI(agentLoop *agent.AgentLoop) {
	port := defaultInternalAPIPort
	// Check for GHOST_API_PORT first, then fallback to legacy GHOST_INTERNAL_API_PORT
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

	// 1. Health Check
	mux.HandleFunc("/v1/health", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		uptime := time.Since(apiStartTime).Seconds()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "ok",
			"timestamp": time.Now().Unix(),
			"version":   "2.0.0",
			"uptime_s":  int64(uptime),
		})
	}))

	// 2. Chat Endpoint (Streaming)
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
			req.SessionKey = "mobile:default"
		}
		if req.Channel == "" {
			req.Channel = "mobile"
		}
		if req.ChatID == "" {
			req.ChatID = "default"
		}

		logger.InfoCF("internal-api", "Processing chat request", map[string]interface{}{
			"session_key":    req.SessionKey,
			"channel":        req.Channel,
			"content_length": len(req.Content),
			"has_media":      len(req.Media) > 0,
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
			// Legacy support: save b64 to temp file
			if tmp, err := saveBase64ToTemp(m); err == nil {
				mediaPaths = append(mediaPaths, tmp)
			}
		}
		for _, item := range req.MediaItems {
			if tmp, err := saveBase64ToTemp(item.Base64); err == nil {
				mediaPaths = append(mediaPaths, tmp)
			}
		}

		// Process message with streaming callback
		ctx := r.Context()
		
		onChunk := func(chunk string) {
			// JSON encode the chunk string to ensure safe transport
			escaped, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", string(escaped))
			flusher.Flush()
		}

		_, err := agentLoop.ProcessDirectWithChannel(ctx, req.Content, req.SessionKey, req.Channel, req.ChatID, mediaPaths, onChunk, nil)
		if err != nil {
			logger.ErrorCF("internal-api", "Error processing chat", map[string]interface{}{"error": err.Error()})
			escaped, _ := json.Marshal("Error: " + err.Error())
			fmt.Fprintf(w, "data: %s\n\n", string(escaped))
			flusher.Flush()
		} else {
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
		}
	}))
	
	// 3. History
	mux.HandleFunc("/v1/history", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		session := resolveSession(r)
		limit := 50
		offset := 0
		fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
		fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)

		if db == nil {
			http.Error(w, `{"error":"database not available"}`, http.StatusInternalServerError)
			return
		}

		rows, err := db.Query(`
			SELECT id, role, content, created_at, meta 
			FROM messages 
			WHERE session_id = ? AND (archived IS NULL OR archived = 0) 
			ORDER BY created_at DESC 
			LIMIT ? OFFSET ?`, session, limit, offset)
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
			
			// Parse meta for tool calls if needed, or media
			// For now, just basic fields
			messages = append(messages, m)
		}
		
		// Reverse
		for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
			messages[i], messages[j] = messages[j], messages[i]
		}

		total := len(messages)
		if err := db.QueryRow(`
			SELECT COUNT(*) 
			FROM messages 
			WHERE session_id = ? AND (archived IS NULL OR archived = 0)`, session).Scan(&total); err != nil {
			total = len(messages)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"messages": messages,
			"total":    total,
		})
	}))

	// 4. Search
	mux.HandleFunc("/v1/search", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		session := resolveSession(r)
		q := r.URL.Query().Get("q")
		if q == "" {
			json.NewEncoder(w).Encode([]Message{})
			return
		}

		if db == nil {
			http.Error(w, `{"error":"database not available"}`, http.StatusInternalServerError)
			return
		}

		rows, err := db.Query(`
			SELECT id, role, content, created_at 
			FROM messages 
			WHERE session_id = ? AND content LIKE ? AND (archived IS NULL OR archived = 0)
			ORDER BY created_at DESC LIMIT 20`, session, "%"+q+"%")
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"db error: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var messages []Message
		for rows.Next() {
			var m Message
			var createdAt time.Time
			if err := rows.Scan(&m.ID, &m.Role, &m.Content, &createdAt); err != nil {
				continue
			}
			m.Timestamp = createdAt.Unix()
			messages = append(messages, m)
		}
		json.NewEncoder(w).Encode(messages)
	}))

	// 5. Memory Files
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
		
		json.NewEncoder(w).Encode(files)
	}))

	// 6. Memory File Content
	mux.HandleFunc("/v1/memory/file", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name required", 400)
			return
		}
		// Prevent path traversal
		clean := filepath.Clean(name)
		if strings.Contains(clean, "..") || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "\\") {
			http.Error(w, "invalid path", 403)
			return
		}
		
		content, err := os.ReadFile(filepath.Join(memoryDir, clean))
		if err != nil {
			http.Error(w, "file not found", 404)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"content": string(content)})
	}))

	// 7. Transcribe
	mux.HandleFunc("/v1/transcribe", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		file, _, err := r.FormFile("audio")
		if err != nil {
			http.Error(w, "audio field required", 400)
			return
		}
		defer file.Close()

		// Forward to Moonshot
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
		if err != nil {
			http.Error(w, "upstream error", 502)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			http.Error(w, "upstream error", 502)
			return
		}

		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, "upstream error", 502)
			return
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(respBytes, &parsed); err == nil {
			if text, ok := parsed["text"].(string); ok {
				json.NewEncoder(w).Encode(map[string]string{"text": text})
				return
			}
		}

		http.Error(w, "upstream error", 502)
	}))

	// 8. Upload
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

	// 9. Delete Message
	mux.HandleFunc("/v1/message", authMiddleware(secret, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", 405)
			return
		}
		id := r.URL.Query().Get("id")
		session := resolveSession(r)
		
		if db != nil {
			// Soft delete (archive)
			db.Exec("UPDATE messages SET archived = 1 WHERE id = ? AND session_id = ?", id, session)
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	
	// 10. Delete Session (Clear History)
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

	// WebSocket
	mux.HandleFunc("/v1/exec", authMiddleware(secret, handleExec(allowedCmds)))
	mux.HandleFunc("/v1/screenshot", authMiddleware(secret, handleScreenshot(screenshotCmd)))
	mux.HandleFunc("/v1/stats", authMiddleware(secret, handleStats))
	mux.HandleFunc("/v1/open", authMiddleware(secret, handleOpen))
	mux.HandleFunc("/v1/ws", handleWebSocket(agentLoop))

	addr := fmt.Sprintf("0.0.0.0:%d", port)
	log.Printf("🤖 Ghost Internal API listening on %s (chat + tools)", addr)
	
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("❌ Internal API failed: %v", err)
	}
}

func saveBase64ToTemp(b64 string) (string, error) {
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
