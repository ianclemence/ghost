// Ghost Internal API — lightweight HTTP server for bridge-to-agent routing
// Exposes ProcessDirectWithChannel() via HTTP so ghost-bridge can route messages
// through the full Ghost agent runtime (tools, RAG, memory, skills).
//
// Listens on localhost only — never exposed to the network.
// Protected by BRIDGE_SECRET header matching.

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/logger"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func handleWebSocket(agentLoop *agent.AgentLoop) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

type internalAPIResponse struct {
	Content   string `json:"content"`
	Error     string `json:"error,omitempty"`
	DurationMs int64 `json:"duration_ms"`
}

// startInternalAPI starts the internal API server for bridge-to-agent routing.
// It listens only on localhost and is protected by BRIDGE_SECRET.
func startInternalAPI(agentLoop *agent.AgentLoop) {
	port := defaultInternalAPIPort
	if p := os.Getenv("GHOST_INTERNAL_API_PORT"); p != "" {
		fmt.Sscanf(p, "%d", &port)
	}

	secret := os.Getenv("BRIDGE_SECRET")
	if secret == "" {
		secret = os.Getenv("GHOST_BRIDGE_SECRET")
	}

	mux := http.NewServeMux()

	// Chat endpoint — processes messages through the full agent runtime
	mux.HandleFunc("/v1/chat", func(w http.ResponseWriter, r *http.Request) {
		// Auth check
		if secret != "" {
			reqSecret := r.Header.Get("X-Ghost-Secret")
			if reqSecret == "" {
				reqSecret = r.Header.Get("Authorization")
			}
			if reqSecret != secret && reqSecret != "Bearer "+secret {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
		}

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

		start := time.Now()

		// Save media to temp files if provided as base64
		mediaPaths := []string{}
		// Handle legacy media array (just b64 strings)
		for _, m := range req.Media {
			if strings.HasPrefix(m, "data:") || len(m) > 100 { // Likely base64
				path, err := saveBase64ToTemp(m, "", "")
				if err == nil {
					mediaPaths = append(mediaPaths, path)
				} else {
					logger.WarnCF("internal-api", "Failed to save legacy base64 media", map[string]interface{}{"error": err.Error()})
				}
			} else if _, err := os.Stat(m); err == nil {
				mediaPaths = append(mediaPaths, m) // Already a path
			}
		}

		// Handle new media_items array (with metadata)
		for _, item := range req.MediaItems {
			path, err := saveBase64ToTemp(item.Base64, item.MimeType, item.Filename)
			if err == nil {
				mediaPaths = append(mediaPaths, path)
			} else {
				logger.WarnCF("internal-api", "Failed to save base64 media item", map[string]interface{}{
					"error":    err.Error(),
					"filename": item.Filename,
					"mime":     item.MimeType,
				})
			}
		}

		// Use a timeout context to prevent indefinite hangs
		ctx, cancel := context.WithTimeout(r.Context(), 300*time.Second)
		defer cancel()

		content := req.Content

		flusher, ok := w.(http.Flusher)
		if !ok {
			// Fallback: wait for full response and write as JSON
			response, err := agentLoop.ProcessDirectWithChannel(
				ctx,
				content,
				req.SessionKey,
				req.Channel,
				req.ChatID,
				mediaPaths,
				nil,
				nil,
			)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(internalAPIResponse{
				Content:    response,
				DurationMs: time.Since(start).Milliseconds(),
			})
			return
		}

		// Stream the response as SSE
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		onChunk := func(chunk string) {
			escaped, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", string(escaped))
			flusher.Flush()
		}

		onToolCall := func(name string, args string) {
			// User requested no "using tool" status lines in the mobile app.
			// We skip sending these status chunks entirely.
		}

		response, err := agentLoop.ProcessDirectWithChannel(
			ctx,
			content,
			req.SessionKey,
			req.Channel,
			req.ChatID,
			mediaPaths,
			onChunk,
			onToolCall,
		)

		duration := time.Since(start)

		if err != nil {
			logger.ErrorCF("internal-api", "Agent processing failed", map[string]interface{}{
				"error":       err.Error(),
				"duration_ms": duration.Milliseconds(),
			})
			// We already started streaming, so we can only send an error in data format or just end
			fmt.Fprintf(w, "data: %s\n\n", `{"error":"`+err.Error()+`"}`)
			flusher.Flush()
			return
		}

		logger.InfoCF("internal-api", "Chat request completed", map[string]interface{}{
			"response_length": len(response),
			"duration_ms":     duration.Milliseconds(),
		})

		// Final sanitize if needed (though it's harder with streaming)
		// For now, just send [DONE]
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})

	// Health check for the internal API
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "ok",
			"type":      "ghost-internal-api",
			"timestamp": time.Now().Unix(),
		})
	})

	mux.HandleFunc("/v1/ws", handleWebSocket(agentLoop))

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 310 * time.Second, // Slightly above the agent processing timeout
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("🔌 Internal API server listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("❌ Internal API server error: %v", err)
		}
	}()
}

func sanitizeMobileResponse(input string) string {
	if strings.TrimSpace(input) == "" {
		return input
	}
	lines := strings.Split(input, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "tool call:") {
			continue
		}
		if strings.Contains(lower, "tool execution started") || strings.Contains(lower, "tool execution failed") || strings.Contains(lower, "tool execution completed") {
			continue
		}
		if strings.Contains(lower, "command blocked by safety guard") {
			continue
		}
		if strings.HasPrefix(lower, "fetched ") && strings.Contains(lower, "bytes") {
			continue
		}
		if strings.HasPrefix(lower, "error:") && strings.Contains(lower, "safety guard") {
			continue
		}
		out = append(out, line)
	}
	cleaned := strings.TrimSpace(strings.Join(out, "\n"))
	if cleaned == "" {
		return "I completed your request, but I can only share user-facing results in mobile mode."
	}
	return cleaned
}

func saveBase64ToTemp(b64Data string, mimeType string, originalName string) (string, error) {
	// Strip data URL prefix if present (e.g., data:image/png;base64,)
	if idx := strings.Index(b64Data, ","); idx != -1 {
		if mimeType == "" {
			// Extract mime from data URL if not provided
			mimePart := b64Data[:idx]
			if strings.HasPrefix(mimePart, "data:") {
				mimeType = strings.Split(strings.TrimPrefix(mimePart, "data:"), ";")[0]
			}
		}
		b64Data = b64Data[idx+1:]
	}

	data, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		return "", err
	}

	tempDir := filepath.Join(os.TempDir(), "picoclaw_media")
	if err := os.MkdirAll(tempDir, 0700); err != nil {
		return "", err
	}

	// Determine extension
	ext := ".bin"
	if originalName != "" {
		ext = filepath.Ext(originalName)
	} else if mimeType != "" {
		switch mimeType {
		case "image/jpeg", "image/jpg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		case "application/pdf":
			ext = ".pdf"
		case "text/plain":
			ext = ".txt"
		case "text/markdown":
			ext = ".md"
		}
	} else if len(data) > 4 && string(data[:4]) == "%PDF" {
		ext = ".pdf"
	}

	filename := uuid.New().String() + ext
	path := filepath.Join(tempDir, filename)

	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}

	return path, nil
}
