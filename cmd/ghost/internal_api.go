// Ghost Internal API — lightweight HTTP server for bridge-to-agent routing
// Exposes ProcessDirectWithChannel() via HTTP so ghost-bridge can route messages
// through the full Ghost agent runtime (tools, RAG, memory, skills).
//
// Listens on localhost only — never exposed to the network.
// Protected by BRIDGE_SECRET header matching.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/logger"
)

const defaultInternalAPIPort = 8766

type internalAPIRequest struct {
	Content    string   `json:"content"`
	SessionKey string   `json:"session_key"`
	Media      []string `json:"media,omitempty"`
	Channel    string   `json:"channel,omitempty"`
	ChatID     string   `json:"chat_id,omitempty"`
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

		// Use a timeout context to prevent indefinite hangs
		ctx, cancel := context.WithTimeout(r.Context(), 300*time.Second)
		defer cancel()

		content := req.Content
		if req.Channel == "mobile" {
			content = "User request:\n" + req.Content + "\n\nReturn only the final user-facing answer. Do not include tool calls, command logs, safety guard messages, raw fetched payloads, or internal debugging text."
		}

		response, err := agentLoop.ProcessDirectWithChannel(
			ctx,
			content,
			req.SessionKey,
			req.Channel,
			req.ChatID,
		)

		duration := time.Since(start)

		if err != nil {
			logger.ErrorCF("internal-api", "Agent processing failed", map[string]interface{}{
				"error":       err.Error(),
				"duration_ms": duration.Milliseconds(),
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(internalAPIResponse{
				Error:      err.Error(),
				DurationMs: duration.Milliseconds(),
			})
			return
		}

		response = sanitizeMobileResponse(response)

		logger.InfoCF("internal-api", "Chat request completed", map[string]interface{}{
			"response_length": len(response),
			"duration_ms":     duration.Milliseconds(),
		})

		// Stream the response as SSE (compatible with bridge's existing SSE parsing)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Ghost-Duration-Ms", fmt.Sprintf("%d", duration.Milliseconds()))

		flusher, ok := w.(http.Flusher)
		if !ok {
			// Fallback: write as JSON
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(internalAPIResponse{
				Content:    response,
				DurationMs: duration.Milliseconds(),
			})
			return
		}

		// Write response as SSE chunks (one big chunk for now, can be improved later)
		// Break into smaller chunks to simulate streaming for better UX
		chunkSize := 80
		for i := 0; i < len(response); i += chunkSize {
			end := i + chunkSize
			if end > len(response) {
				end = len(response)
			}
			chunk := response[i:end]
			escaped, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", string(escaped))
			flusher.Flush()
			// Tiny delay to simulate streaming
			time.Sleep(5 * time.Millisecond)
		}

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
