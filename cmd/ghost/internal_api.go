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

func handleWebSocket(agentLoop *agent.AgentLoop) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Auth check
		secret := r.URL.Query().Get("secret")
		if secret == "" {
			secret = r.Header.Get("X-Ghost-Secret")
		}
		if secret != os.Getenv("BRIDGE_SECRET") && secret != os.Getenv("GHOST_BRIDGE_SECRET") {
			// Fallback for when secret is not set in env (dev mode)
			if os.Getenv("BRIDGE_SECRET") != "" || os.Getenv("GHOST_BRIDGE_SECRET") != "" {
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

const defaultInternalAPIPort = 8765

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
	if secret == "" {
		secret = os.Getenv("GHOST_BRIDGE_SECRET")
	}

	memoryDir := os.Getenv("MEMORY_DIR")
	if memoryDir == "" {
		home := os.Getenv("HOME")
		memoryDir = filepath.Join(home, "ghost", "workspace", "memory")
	}

	db := agentLoop.DB()

	mux := http.NewServeMux()

	// Auth Middleware
	withAuth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Ghost-Secret, Authorization")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			if secret != "" {
				reqSecret := r.Header.Get("X-Ghost-Secret")
				if reqSecret == "" {
					reqSecret = r.Header.Get("Authorization")
					if strings.HasPrefix(reqSecret, "Bearer ") {
						reqSecret = strings.TrimPrefix(reqSecret, "Bearer ")
					}
				}
				if reqSecret != secret {
					http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
					return
				}
			}
			next(w, r)
		}
	}

	resolveSession := func(r *http.Request) string {
		if s := r.Header.Get("X-Ghost-Session"); s != "" {
			return s
		}
		if s := r.URL.Query().Get("session"); s != "" {
			return s
		}
		return "mobile:default"
	}

	// 1. Health Check
	mux.HandleFunc("/v1/health", withAuth(func(w http.ResponseWriter, r *http.Request) {
		uptime := time.Since(time.Now()).Seconds() // Approximate, ideally use app startup time
		// Since we don't have global startup time here easily, we'll use 0 or pass it in.
		// For now, let's just return a static valid response.
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "ok",
			"timestamp": time.Now().Unix(),
			"version":   "2.0.0",
			"uptime_s":  int64(uptime),
		})
	}))

	// 2. Chat Endpoint (Streaming)
	mux.HandleFunc("/v1/chat", withAuth(func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/v1/history", withAuth(func(w http.ResponseWriter, r *http.Request) {
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

		json.NewEncoder(w).Encode(map[string]interface{}{
			"messages": messages,
			"total":    len(messages), // Approximation
		})
	}))

	// 4. Search
	mux.HandleFunc("/v1/search", withAuth(func(w http.ResponseWriter, r *http.Request) {
		session := resolveSession(r)
		q := r.URL.Query().Get("q")
		if q == "" {
			json.NewEncoder(w).Encode(map[string]interface{}{"messages": []Message{}})
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
		json.NewEncoder(w).Encode(map[string]interface{}{"messages": messages})
	}))

	// 5. Memory Files
	mux.HandleFunc("/v1/memory/files", withAuth(func(w http.ResponseWriter, r *http.Request) {
		type FileInfo struct {
			Name     string `json:"name"`
			Modified int64  `json:"modified"`
			Size     int64  `json:"size"`
		}
		var files []FileInfo
		
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
	mux.HandleFunc("/v1/memory/file", withAuth(func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/v1/transcribe", withAuth(func(w http.ResponseWriter, r *http.Request) {
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
		part, _ := writer.CreateFormFile("file", "audio.webm") // Default name
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
		
		io.Copy(w, resp.Body)
	}))

	// 8. Upload
	mux.HandleFunc("/v1/upload", withAuth(func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/v1/message", withAuth(func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/v1/messages", withAuth(func(w http.ResponseWriter, r *http.Request) {
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
