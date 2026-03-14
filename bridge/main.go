package main

import (
	"bufio"
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
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	_ "modernc.org/sqlite"
)

type Config struct {
	Port          string   `json:"port"`
	GhostDBPath   string   `json:"ghost_db_path"`
	GhostAgentURL string   `json:"ghost_agent_url"`
	KimiAPIKey    string   `json:"kimi_api_key"`
	BridgeSecret  string   `json:"bridge_secret"`
	MemoryDir     string   `json:"memory_dir"`
	AllowedCmds   []string `json:"allowed_cmds"`
	ScreenshotCmd string   `json:"screenshot_cmd"`
	SystemPrompt  string   `json:"system_prompt"`
}

type Message struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
	MediaType string `json:"media_type,omitempty"`
	MediaURL  string `json:"media_url,omitempty"`
}

type SendRequest struct {
	Content   string `json:"content"`
	MediaB64  string `json:"media_b64,omitempty"`
	MediaType string `json:"media_type,omitempty"`
}

type HistoryResponse struct {
	Messages []Message `json:"messages"`
	Total    int       `json:"total"`
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
var db *sql.DB
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}
var sessionID = "mobile:default"
var startupTime = time.Now()

var wsClients = map[string]*websocket.Conn{}
var wsMu sync.Mutex

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

func handleHealth(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
		"version":   "1.1.0",
		"uptime_s":  int(time.Since(startupTime).Seconds()),
		"started":   startupTime.Unix(),
	})
}

func handleHistory(w http.ResponseWriter, r *http.Request) {
	if db == nil {
		log.Println("❌ DB is nil in handleHistory")
		http.Error(w, `{"error":"database not initialized"}`, 500)
		return
	}
	limit := 50
	offset := 0
	_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
	_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)

	rows, err := db.Query(
		`SELECT id, role, content, CAST(strftime('%s', created_at) AS INTEGER)
		 FROM messages
		 WHERE session_id = ? AND (archived IS NULL OR archived = 0)
		 ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		sessionID, limit, offset,
	)
	if err != nil {
		log.Printf("❌ handleHistory query error: %v", err)
		http.Error(w, `{"error":"db error"}`, 500)
		return
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		_ = rows.Scan(&m.ID, &m.Role, &m.Content, &m.Timestamp)
		msgs = append(msgs, m)
	}
	if msgs == nil {
		msgs = []Message{}
	}

	var total int
	_ = db.QueryRow(
		`SELECT COUNT(*) FROM messages WHERE session_id = ? AND (archived IS NULL OR archived = 0)`,
		sessionID,
	).Scan(&total)

	_ = json.NewEncoder(w).Encode(HistoryResponse{Messages: msgs, Total: total})
}

func handleSend(w http.ResponseWriter, r *http.Request) {
	if db == nil {
		log.Println("❌ DB is nil in handleSend")
		http.Error(w, `{"error":"database not initialized"}`, 500)
		return
	}
	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, 400)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", 500)
		return
	}

	useAgentRoute := cfg.GhostAgentURL != "" && req.MediaB64 == ""
	if useAgentRoute {
		if err := streamAgentResponse(w, flusher, req.Content); err == nil {
			return
		} else {
			log.Printf("⚠️ Agent route failed, falling back to direct Kimi: %v", err)
		}
	}

	ensureSession()
	msgID := uuid.NewString()
	metaJSON, _ := json.Marshal(map[string]interface{}{})
	_, err := db.Exec(
		`INSERT INTO messages (id, session_id, role, content, meta, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		msgID, sessionID, "user", req.Content, metaJSON, time.Now(),
	)
	if err != nil {
		log.Printf("❌ DB insert failed: %v", err)
		http.Error(w, `{"error":"db error"}`, 500)
		return
	}
	_, _ = db.Exec(`UPDATE sessions SET updated_at = ? WHERE id = ?`, time.Now(), sessionID)

	messages := buildContextMessages(req.Content, req.MediaB64, req.MediaType)
	streamKimiResponse(w, flusher, messages)
}

func streamAgentResponse(w http.ResponseWriter, flusher http.Flusher, content string) error {
	baseURL := strings.TrimSpace(cfg.GhostAgentURL)
	if baseURL == "" {
		return fmt.Errorf("ghost agent url is empty")
	}
	baseURL = strings.TrimRight(baseURL, "/")
	bodyBytes, err := json.Marshal(map[string]interface{}{
		"content":     content,
		"session_key": sessionID,
		"channel":     "mobile",
		"chat_id":     "default",
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		baseURL+"/v1/chat",
		bytes.NewReader(bodyBytes),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.BridgeSecret != "" {
		req.Header.Set("X-Ghost-Secret", cfg.BridgeSecret)
	}

	client := &http.Client{Timeout: 320 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	log.Printf("🔁 Agent route response: status=%d content-type=%s", resp.StatusCode, resp.Header.Get("Content-Type"))

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agent route http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		var payload struct {
			Content string `json:"content"`
			Error   string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return err
		}
		if payload.Error != "" {
			return fmt.Errorf("agent route error: %s", payload.Error)
		}
		if payload.Content == "" {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", jsonEscape("No content returned by Ghost agent"))
			_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return nil
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", jsonEscape(payload.Content))
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return nil
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	hasData := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		hasData = true
		data := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if data == "[DONE]" {
			_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return nil
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !hasData {
		return fmt.Errorf("agent route returned no sse data")
	}
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
	return nil
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if cfg.BridgeSecret != "" {
		secret := r.URL.Query().Get("secret")
		if secret == "" {
			secret = r.Header.Get("X-Ghost-Secret")
		}
		if secret != cfg.BridgeSecret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	clientID := fmt.Sprintf("%d", time.Now().UnixNano())
	wsMu.Lock()
	wsClients[clientID] = conn
	wsMu.Unlock()
	defer func() {
		wsMu.Lock()
		delete(wsClients, clientID)
		wsMu.Unlock()
	}()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func handleMemoryFiles(w http.ResponseWriter, r *http.Request) {
	var files []map[string]interface{}
	_ = filepath.WalkDir(cfg.MemoryDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		rel, relErr := filepath.Rel(cfg.MemoryDir, path)
		if relErr != nil {
			return nil
		}
		files = append(files, map[string]interface{}{
			"name":     filepath.ToSlash(rel),
			"modified": info.ModTime().Unix(),
			"size":     info.Size(),
		})
		return nil
	})
	if files == nil {
		files = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(files)
}

func handleMemoryFile(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, `{"error":"not found"}`, 404)
		return
	}
	clean := filepath.Clean(name)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		http.Error(w, `{"error":"invalid path"}`, 400)
		return
	}
	path := filepath.Join(cfg.MemoryDir, clean)
	rel, err := filepath.Rel(cfg.MemoryDir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		http.Error(w, `{"error":"invalid path"}`, 400)
		return
	}
	content, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, `{"error":"not found"}`, 404)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"content": string(content)})
}

// ─── FIX: buildContextMessages ────────────────────────────────────────────
// The original crash was caused by a nil *sql.Rows being iterated.
// The fallback query assigned to `rows` but the outer `err` check masked it.
// Now we use a single safe helper that always returns a valid (possibly empty) slice.

func queryHistory() []map[string]interface{} {
	if db == nil {
		return nil
	}

	// Try with archived filter first (new schema)
	rows, err := db.Query(
		`SELECT role, content FROM messages
		 WHERE session_id = ? AND (archived IS NULL OR archived = 0)
		 ORDER BY created_at DESC LIMIT 20`,
		sessionID,
	)
	if err != nil {
		// Schema might not have 'archived' column — fall back to simpler query
		log.Printf("⚠️ History query (with archived filter) failed: %v — retrying without filter", err)
		rows, err = db.Query(
			`SELECT role, content FROM messages
			 WHERE session_id = ?
			 ORDER BY created_at DESC LIMIT 20`,
			sessionID,
		)
		if err != nil {
			log.Printf("❌ History fallback query also failed: %v", err)
			return nil
		}
	}
	// rows is guaranteed non-nil here
	defer rows.Close()

	var history []map[string]interface{}
	for rows.Next() {
		var role, content string
		if scanErr := rows.Scan(&role, &content); scanErr != nil {
			log.Printf("⚠️ Row scan error: %v", scanErr)
			continue
		}
		// Prepend to reverse chronological → chronological order
		history = append([]map[string]interface{}{{"role": role, "content": content}}, history...)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		log.Printf("⚠️ Rows iteration error: %v", rowsErr)
	}
	return history
}

func buildContextMessages(userText, mediaB64, mediaType string) []map[string]interface{} {
	history := queryHistory() // safe — always returns slice or nil

	var userContent interface{}
	if mediaB64 != "" && strings.HasPrefix(mediaType, "image/") {
		userContent = []map[string]interface{}{
			{"type": "image_url", "image_url": map[string]string{
				"url": fmt.Sprintf("data:%s;base64,%s", mediaType, mediaB64),
			}},
			{"type": "text", "text": userText},
		}
	} else {
		userContent = userText
	}

	msgs := append(history, map[string]interface{}{
		"role":    "user",
		"content": userContent,
	})

	if cfg.SystemPrompt != "" {
		msgs = append([]map[string]interface{}{
			{"role": "system", "content": cfg.SystemPrompt},
		}, msgs...)
	}
	return msgs
}

func streamKimiResponse(w http.ResponseWriter, flusher http.Flusher, messages []map[string]interface{}) {
	if cfg.KimiAPIKey == "" {
		log.Println("❌ Kimi API key is missing")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", jsonEscape("Kimi API key is missing. Please set KIMI_API_KEY in .env"))
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	payload, err := json.Marshal(map[string]interface{}{
		"model":    getEnv("GHOST_AGENTS_DEFAULTS_MODEL", "kimi-k2.5"),
		"messages": messages,
		"stream":   true,
	})
	if err != nil {
		log.Printf("❌ Failed to marshal payload: %v", err)
		_, _ = fmt.Fprintf(w, "data: {\"error\":\"Failed to marshal payload: %s\"}\n\n", err.Error())
		flusher.Flush()
		return
	}

	req, err := http.NewRequestWithContext(context.Background(), "POST",
		"https://api.moonshot.ai/v1/chat/completions",
		strings.NewReader(string(payload)),
	)
	if err != nil {
		log.Printf("❌ Failed to create request: %v", err)
		_, _ = fmt.Fprintf(w, "data: {\"error\":\"Failed to create request: %s\"}\n\n", err.Error())
		flusher.Flush()
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.KimiAPIKey)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", jsonEscape("Upstream request failed: "+err.Error()))
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}
	defer resp.Body.Close()
	log.Printf("🌐 Upstream response: status=%d content-type=%s", resp.StatusCode, resp.Header.Get("Content-Type"))

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		body := strings.TrimSpace(string(bodyBytes))
		if len(body) > 400 {
			body = body[:400]
		}
		msg := fmt.Sprintf("Upstream HTTP %d", resp.StatusCode)
		if body != "" {
			msg = msg + ": " + body
		}
		log.Printf("❌ %s", msg)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", jsonEscape(msg))
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	var fullResponse strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	hasTextChunk := false
	receivedDone := false
	upstreamError := ""
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			receivedDone = true
			break
		}

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			log.Printf("⚠️ Upstream chunk parse error: %v", err)
			continue
		}

		if errField, ok := chunk["error"].(map[string]interface{}); ok {
			if m, ok := errField["message"].(string); ok && m != "" {
				upstreamError = m
				log.Printf("❌ Upstream error: %s", m)
			}
			continue
		}

		text := extractStreamText(chunk)
		if text != "" {
			hasTextChunk = true
			fullResponse.WriteString(text)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", jsonEscape(text))
			flusher.Flush()
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		log.Printf("❌ Upstream scan error: %v", scanErr)
		if upstreamError == "" {
			upstreamError = scanErr.Error()
		}
	}

	if !hasTextChunk {
		msg := "No text chunks received from upstream stream"
		if upstreamError != "" {
			msg += ": " + upstreamError
		}
		log.Printf("⚠️ %s", msg)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", jsonEscape(msg))
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	if receivedDone {
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}

	if fullResponse.Len() > 0 {
		if db == nil {
			log.Println("❌ DB is nil in streamKimiResponse")
			return
		}
		ensureSession()
		metaJSON, _ := json.Marshal(map[string]interface{}{})
		_, _ = db.Exec(
			`INSERT INTO messages (id, session_id, role, content, meta, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			uuid.NewString(), sessionID, "assistant", fullResponse.String(), metaJSON, time.Now(),
		)
		_, _ = db.Exec(`UPDATE sessions SET updated_at = ? WHERE id = ?`, time.Now(), sessionID)

		broadcastToWS(map[string]interface{}{
			"type":    "assistant_message",
			"content": fullResponse.String(),
		})
	}
}

func extractStreamText(chunk map[string]interface{}) string {
	choices, ok := chunk["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return ""
	}
	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return ""
	}
	if delta, ok := choice["delta"].(map[string]interface{}); ok {
		if content := extractContentText(delta["content"]); content != "" {
			return content
		}
	}
	if message, ok := choice["message"].(map[string]interface{}); ok {
		if content := extractContentText(message["content"]); content != "" {
			return content
		}
	}
	if text, ok := choice["text"].(string); ok && text != "" {
		return text
	}
	return ""
}

func extractContentText(v interface{}) string {
	switch c := v.(type) {
	case string:
		return c
	case []interface{}:
		var b strings.Builder
		for _, part := range c {
			switch p := part.(type) {
			case string:
				b.WriteString(p)
			case map[string]interface{}:
				if text, ok := p["text"].(string); ok {
					b.WriteString(text)
				}
			}
		}
		return b.String()
	}
	return ""
}

func broadcastToWS(msg interface{}) {
	data, _ := json.Marshal(msg)
	wsMu.Lock()
	defer wsMu.Unlock()
	for id, conn := range wsClients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			_ = conn.Close()
			delete(wsClients, id)
		}
	}
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func initDB() {
	dbPath := cfg.GhostDBPath
	log.Printf("📂 Opening database at: %s", dbPath)

	dir := filepath.Dir(dbPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		log.Printf("📁 Creating database directory: %s", dir)
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("❌ Failed to create DB directory: %v", err)
		}
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	var err error
	db, err = sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("❌ Failed to open DB: %v", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatalf("❌ Failed to ping DB: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id         TEXT PRIMARY KEY,
			created_at DATETIME,
			updated_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS messages (
			id         TEXT PRIMARY KEY,
			session_id TEXT,
			role       TEXT,
			content    TEXT,
			meta       TEXT,
			archived   INTEGER DEFAULT 0,
			created_at DATETIME,
			FOREIGN KEY(session_id) REFERENCES sessions(id)
		);
	`)
	if err != nil {
		log.Fatalf("❌ Failed to create tables: %v", err)
	}

	// Ensure the 'archived' column exists (in case DB was created by an older version)
	_, _ = db.Exec(`ALTER TABLE messages ADD COLUMN archived INTEGER DEFAULT 0`)

	log.Println("✅ Database initialized successfully")
}

func ensureSession() {
	if db == nil {
		log.Println("❌ DB is nil in ensureSession")
		return
	}
	_, _ = db.Exec(
		`INSERT OR IGNORE INTO sessions (id, created_at, updated_at) VALUES (?, ?, ?)`,
		sessionID, time.Now(), time.Now(),
	)
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseMultipartForm(10 << 20)
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"no file"}`, 400)
		return
	}
	defer file.Close()

	data, _ := io.ReadAll(file)
	b64 := base64.StdEncoding.EncodeToString(data)
	mimeType := header.Header.Get("Content-Type")

	_ = json.NewEncoder(w).Encode(map[string]string{
		"b64":       b64,
		"mime_type": mimeType,
		"filename":  header.Filename,
	})
}

func handleTranscribe(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseMultipartForm(25 << 20)
	file, header, err := r.FormFile("audio")
	if err != nil {
		http.Error(w, `{"error":"no audio file"}`, 400)
		return
	}
	defer file.Close()

	audioBytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, `{"error":"read error"}`, 500)
		return
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", header.Filename)
	_, _ = fw.Write(audioBytes)
	_ = mw.WriteField("model", "whisper-1")
	_ = mw.WriteField("language", "en")
	_ = mw.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "POST",
		"https://api.moonshot.ai/v1/audio/transcriptions", &buf,
	)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+cfg.KimiAPIKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "", "error": err.Error()})
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	text := ""
	if t, ok := result["text"].(string); ok {
		text = strings.TrimSpace(t)
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"text": text})
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
	if scmdStr == "" {
		for _, tool := range []string{"scrot", "gnome-screenshot", "import"} {
			if _, err := exec.LookPath(tool); err == nil {
				switch tool {
				case "scrot":
					scmdStr = "scrot " + outPath
				case "gnome-screenshot":
					scmdStr = "gnome-screenshot -f " + outPath
				case "import":
					scmdStr = "import -window root " + outPath
				}
				break
			}
		}
	}
	if scmdStr == "" {
		http.Error(w, `{"error":"no screenshot tool"}`, 500)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", "DISPLAY=:0 "+scmdStr)
	cmd.Env = append(os.Environ(), "DISPLAY=:0")
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"screenshot failed: %s"}`, err.Error()), 500)
		return
	}

	imgBytes, err := os.ReadFile(outPath)
	if err != nil {
		http.Error(w, `{"error":"could not read screenshot"}`, 500)
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

func handleSearch(w http.ResponseWriter, r *http.Request) {
	if db == nil {
		http.Error(w, `{"error":"database not initialized"}`, 500)
		return
	}
	q := "%" + r.URL.Query().Get("q") + "%"
	limit := 20
	_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)

	rows, err := db.Query(
		`SELECT id, role, content, CAST(strftime('%s', created_at) AS INTEGER)
		 FROM messages
		 WHERE session_id = ? AND (archived IS NULL OR archived = 0) AND content LIKE ?
		 ORDER BY created_at DESC LIMIT ?`,
		sessionID, q, limit,
	)
	if err != nil {
		log.Printf("❌ handleSearch query error: %v", err)
		http.Error(w, `{"error":"db error"}`, 500)
		return
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		_ = rows.Scan(&m.ID, &m.Role, &m.Content, &m.Timestamp)
		msgs = append(msgs, m)
	}
	if msgs == nil {
		msgs = []Message{}
	}
	_ = json.NewEncoder(w).Encode(msgs)
}

func handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	if db == nil {
		http.Error(w, `{"error":"database not initialized"}`, 500)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, `{"error":"method not allowed"}`, 405)
		return
	}
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, `{"error":"invalid id"}`, 400)
		return
	}
	_, _ = db.Exec(`DELETE FROM messages WHERE id = ? AND session_id = ?`, idStr, sessionID)
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func handleClearMessages(w http.ResponseWriter, r *http.Request) {
	if db == nil {
		http.Error(w, `{"error":"database not initialized"}`, 500)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, `{"error":"method not allowed"}`, 405)
		return
	}
	// Archive instead of delete — preserves data, matches core Ghost behavior
	_, err := db.Exec(`UPDATE messages SET archived = 1 WHERE session_id = ? AND (archived IS NULL OR archived = 0)`, sessionID)
	if err != nil {
		log.Printf("❌ handleClearMessages archive error: %v", err)
		http.Error(w, `{"error":"db error"}`, 500)
		return
	}
	_, _ = db.Exec(`UPDATE sessions SET summary = '', updated_at = ? WHERE id = ?`, time.Now(), sessionID)
	log.Printf("✅ Chat messages archived for session %s", sessionID)
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func main() {
	// Load .env before reading config. Try several candidate paths so it works
	// whether run manually from the bridge/ dir or as a systemd service.
	// Priority: explicit ENV_FILE var > sibling .env > parent .env > grandparent .env
	// Resolve the .env file. Try multiple locations so it works for any user
	// without hardcoding a username anywhere.
	home := os.Getenv("HOME")
	envCandidates := []string{
		os.Getenv("ENV_FILE"),                         // explicit override via ENV_FILE=
		".env",                                        // running from bridge/ dir directly
		"../.env",                                     // bridge/ is inside ghost/ → ../ghost root
		filepath.Join(home, "ghost", ".env"),          // ~/ghost/.env  (absolute, any user)
		filepath.Join(home, ".env"),                   // ~/.env        (last resort)
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

	// Build HOME-relative default paths so no username is hardcoded.
	// These are only used when the .env doesn't supply explicit values.
	defaultDB := filepath.Join(home, "ghost", "workspace", "ghost.db")
	defaultMem := filepath.Join(home, "ghost", "workspace", "memory")

	cfg = Config{
		Port:          getEnv("BRIDGE_PORT", "8765"),
		GhostDBPath:   getEnv("GHOST_DB_PATH", defaultDB),
		GhostAgentURL: getEnv("GHOST_AGENT_URL", ""),
		KimiAPIKey:    getEnv("KIMI_API_KEY", ""),
		BridgeSecret:  getEnv("BRIDGE_SECRET", ""),
		MemoryDir:     getEnv("MEMORY_DIR", defaultMem),
		ScreenshotCmd: getEnv("SCREENSHOT_CMD", ""),
		SystemPrompt:  getEnv("GHOST_SYSTEM_PROMPT", "You are Ghost, a sovereign AI assistant running on a Raspberry Pi. Be concise and direct."),
	}

	// Log startup diagnostic — shows whether critical keys were found
	if cfg.KimiAPIKey != "" {
		log.Printf("🔑 KIMI_API_KEY loaded (len=%d)", len(cfg.KimiAPIKey))
	} else {
		log.Println("❌ KIMI_API_KEY is EMPTY — check your .env file and loadDotEnv path")
	}

	if cfg.BridgeSecret == "" {
		log.Println("⚠️  WARNING: BRIDGE_SECRET is not set. Using default 'ghost-pi-secret'.")
		cfg.BridgeSecret = "ghost-pi-secret"
	}

	if raw := getEnv("ALLOWED_CMDS", ""); raw != "" {
		cfg.AllowedCmds = strings.Split(raw, ",")
	}

	initDB()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", authMiddleware(handleHealth))
	mux.HandleFunc("/history", authMiddleware(handleHistory))
	mux.HandleFunc("/send", authMiddleware(handleSend))
	mux.HandleFunc("/upload", authMiddleware(handleUpload))
	mux.HandleFunc("/transcribe", authMiddleware(handleTranscribe))
	mux.HandleFunc("/exec", authMiddleware(handleExec))
	mux.HandleFunc("/screenshot", authMiddleware(handleScreenshot))
	mux.HandleFunc("/stats", authMiddleware(handleStats))
	mux.HandleFunc("/open", authMiddleware(handleOpen))
	mux.HandleFunc("/search", authMiddleware(handleSearch))
	mux.HandleFunc("/message", authMiddleware(handleDeleteMessage))
	mux.HandleFunc("/messages", authMiddleware(handleClearMessages))
	mux.HandleFunc("/memory/files", authMiddleware(handleMemoryFiles))
	mux.HandleFunc("/memory/file", authMiddleware(handleMemoryFile))
	mux.HandleFunc("/ws", handleWebSocket)

	addr := "0.0.0.0:" + cfg.Port
	log.Printf("👻 Ghost Bridge running on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// loadDotEnv reads a .env file and populates os.Environ with any key that
// isn't already set. Real env vars (systemd, shell exports) always win.
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
		// Only skip if the env already has a real non-empty value.
		// An empty string means systemd or the shell set it to nothing — overwrite it.
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
