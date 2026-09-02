// Ghost Internal API — lightweight HTTP server for agent routing
// Exposes ProcessDirectWithChannel() via HTTP so the mobile app can talk directly
// to the full Ghost agent runtime (tools, RAG, memory, skills).
//
// Listens on the LAN (0.0.0.0) with layered trust:
//   - Loopback peers (web console proxy, relay client, TUI, CLI) are trusted.
//   - LAN peers must present valid per-device credentials on every request,
//     except the public pairing redemption endpoints where the short-lived
//     pairing token itself is the authorization.

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
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/channels"
	"github.com/ianclemence/ghost/pkg/cron"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/pairing"
	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/scheduled"
	"github.com/ianclemence/ghost/pkg/skills"
	"github.com/ianclemence/ghost/pkg/telemetry"
	"github.com/ianclemence/ghost/pkg/tools"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var apiStartTime = time.Now()

// apiDB is the agent database used by peer authorization. It is set once in
// startInternalAPI so the auth middlewares (which have uniform signatures
// across many call sites) can validate device credentials.
var apiDB *sql.DB

func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// peerAuthorized reports whether the request may proceed.
// Loopback peers (web proxy, relay client, TUI, CLI) are trusted. LAN peers
// must present a valid per-device credential; validation also refreshes the
// device's last-seen timestamp.
func peerAuthorized(r *http.Request) bool {
	if isLoopbackRequest(r) {
		return true
	}
	deviceID := r.Header.Get("X-Ghost-Device-ID")
	credential := r.Header.Get("X-Ghost-Credential")
	if deviceID == "" || credential == "" || apiDB == nil {
		return false
	}
	valid, _ := pairing.ValidateCredential(apiDB, deviceID, credential)
	if valid {
		_ = pairing.UpdateLastSeen(apiDB, deviceID)
	}
	return valid
}

// detectLANAddress returns the IP of the interface that has a default route,
// so pairing invitations carry a usable address instead of a placeholder.
func detectLANAddress() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP == nil {
		return "127.0.0.1"
	}
	return addr.IP.String()
}

func handleWebSocket(agentLoop *agent.AgentLoop) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Loopback peers (relay client, local tools) are trusted. LAN peers
		// must present valid device credentials — credentials are never
		// accepted via query parameters.
		deviceID := r.Header.Get("X-Ghost-Device-ID")
		credential := r.Header.Get("X-Ghost-Credential")
		if !isLoopbackRequest(r) {
			if deviceID == "" || credential == "" {
				http.Error(w, `{"error":{"code":"authentication_required","message":"Device authentication required."}}`, http.StatusUnauthorized)
				return
			}
			if valid, _ := pairing.ValidateCredential(agentLoop.DB(), deviceID, credential); !valid {
				http.Error(w, `{"error":{"code":"authentication_failed","message":"Invalid device credentials."}}`, http.StatusUnauthorized)
				return
			}
			_ = pairing.UpdateLastSeen(agentLoop.DB(), deviceID)
		} else if deviceID != "" && credential != "" {
			if valid, _ := pairing.ValidateCredential(agentLoop.DB(), deviceID, credential); valid {
				_ = pairing.UpdateLastSeen(agentLoop.DB(), deviceID)
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

				// Only forward mobile-channel messages and interactive/tool events
				// to the app. Telegram and CLI responses must not appear in the
				// mobile chat.
				if msg.Channel != "mobile" {
					meta, _ := msg.Metadata["type"].(string)
					switch meta {
					case "canvas_update", "cron_update", "clarify_request", "progress_event":
						// forwarded — interactive/tool events the app renders
					default:
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

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Ghost-Session, X-Ghost-Device-ID, X-Ghost-Credential, X-Ghost-Client-Id, X-Ghost-Client-Token")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// Loopback peers are trusted; LAN peers must present device credentials.
		if !peerAuthorized(r) {
			jsonResponse(w, http.StatusUnauthorized, map[string]interface{}{
				"error": map[string]string{
					"code":    pairing.ErrCodeAuthRequired,
					"message": "Device authentication required.",
				},
			})
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		next(w, r)
	}
}

// deviceAuthMiddleware is authMiddleware with an explicit database handle.
// Used on endpoints where the database may not yet be wired into apiDB.
func deviceAuthMiddleware(db *sql.DB, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Ghost-Session, X-Ghost-Device-ID, X-Ghost-Credential, X-Ghost-Client-Id, X-Ghost-Client-Token")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !isLoopbackRequest(r) {
			deviceID := r.Header.Get("X-Ghost-Device-ID")
			credential := r.Header.Get("X-Ghost-Credential")
			if deviceID == "" || credential == "" || db == nil {
				jsonResponse(w, http.StatusUnauthorized, map[string]interface{}{
					"error": map[string]string{
						"code":    pairing.ErrCodeAuthRequired,
						"message": "Device authentication required.",
					},
				})
				return
			}
			valid, _ := pairing.ValidateCredential(db, deviceID, credential)
			if !valid {
				jsonResponse(w, http.StatusUnauthorized, map[string]interface{}{
					"error": map[string]string{
						"code":    pairing.ErrCodeAuthFailed,
						"message": "Invalid device credentials.",
					},
				})
				return
			}
			_ = pairing.UpdateLastSeen(db, deviceID)
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		next(w, r)
	}
}

// publicHandler wraps a handler with CORS headers but no authentication.
// Used for endpoints that must be accessible without credentials (e.g., pairing/redeem).
func publicHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
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

func isUserVisibleHistoryMessage(role, content string, metaJSON []byte) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	if role != "assistant" {
		return true
	}
	var meta map[string]interface{}
	if len(metaJSON) > 0 {
		_ = json.Unmarshal(metaJSON, &meta)
		if tc, ok := meta["tool_calls"]; ok {
			if arr, ok := tc.([]interface{}); ok && len(arr) > 0 {
				return false
			}
		}
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "<skills>") || strings.Contains(lower, "</skills>") {
		return false
	}
	if strings.Contains(lower, "skills/{skill-name}/skill.md") {
		return false
	}
	if strings.Contains(lower, "```json") && strings.Contains(lower, "\"metadata\"") && strings.Contains(lower, "\"homepage\"") {
		return false
	}
	if strings.HasPrefix(lower, "name:") && strings.Contains(lower, "\ndescription:") {
		return false
	}
	return true
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
	case "vision":
		if u, ok := a["url"].(string); ok && u != "" {
			if len(u) > 40 {
				u = u[:40] + "…"
			}
			return "Analyzing: " + u
		}
		if p, ok := a["path"].(string); ok && p != "" {
			parts := strings.Split(p, "/")
			return "Analyzing: " + parts[len(parts)-1]
		}
		return "Analyzing image…"
	case "image_generate":
		if p, ok := a["prompt"].(string); ok && p != "" {
			if len(p) > 40 {
				p = p[:40] + "…"
			}
			return "Generating image: " + p
		}
		return "Generating image…"
	default:
		return "Using " + name + "…"
	}
}

type internalAPIRequest struct {
	RequestID  string            `json:"request_id,omitempty"`
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
	Label     string `json:"label,omitempty"`
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

type cronCreateRequestBody struct {
	Name     string            `json:"name"`
	Schedule cron.CronSchedule `json:"schedule"`
	Message  string            `json:"message"`
	Command  string            `json:"command"`
	Deliver  bool              `json:"deliver"`
	Channel  string            `json:"channel"`
	To       string            `json:"to"`
	Target   string            `json:"target"`
	Skills   []string          `json:"skills"`
	NoAgent  bool              `json:"no_agent"`
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

// ── Skills helpers ─────────────────────────────────────────────────────────
// These mirror the ghost-web admin console's skill management endpoints but are
// accessible via the unified 8766 API for the mobile app.

// skillSummaryMD extracts a one-line description from a SKILL.md file,
// preferring the frontmatter description field.
func skillSummaryMD(text string) string {
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

// validSkillName guards against path traversal in skill operations.
func validSkillName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		!strings.Contains(name, "/") && !strings.Contains(name, "\\") &&
		!strings.Contains(name, "..")
}

// listWorkspaceSkills returns installed skills (enabled and disabled).
func listWorkspaceSkills(skillsDir string) []map[string]string {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return []map[string]string{}
	}
	manifest, _ := skills.LoadManifest(skillsDir)
	result := []map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		skillPath := filepath.Join(skillsDir, name)
		enabled := false
		desc := ""
		if b, err := os.ReadFile(filepath.Join(skillPath, "SKILL.md")); err == nil {
			enabled = true
			desc = skillSummaryMD(string(b))
		} else if b, err := os.ReadFile(filepath.Join(skillPath, "SKILL.md.disabled")); err == nil {
			enabled = false
			desc = skillSummaryMD(string(b))
		} else {
			continue
		}
		entry, bundled := manifest.Skills[name]
		result = append(result, map[string]string{
			"name":          name,
			"description":   desc,
			"bundled":       strconv.FormatBool(bundled),
			"user_modified": strconv.FormatBool(bundled && entry.UserModified),
			"enabled":       strconv.FormatBool(enabled),
			"optional":      strconv.FormatBool(skills.IsOptionalSkill(name)),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i]["name"] < result[j]["name"] })
	return result
}

// setSkillEnabled enables/disables a skill by renaming SKILL.md <-> SKILL.md.disabled.
func setSkillEnabled(skillsDir, name string, enabled bool) error {
	if !validSkillName(name) {
		return fmt.Errorf("invalid skill name")
	}
	skillPath := filepath.Join(skillsDir, name)
	if !strings.HasPrefix(skillPath, skillsDir+string(filepath.Separator)) {
		return fmt.Errorf("invalid skill name")
	}
	src := filepath.Join(skillPath, "SKILL.md")
	dst := filepath.Join(skillPath, "SKILL.md.disabled")
	if enabled {
		if _, err := os.Stat(dst); err == nil {
			return os.Rename(dst, src)
		}
		return nil
	}
	if _, err := os.Stat(src); err == nil {
		return os.Rename(src, dst)
	}
	return fmt.Errorf("skill not found")
}

// readSkillDetail returns a skill's files and metadata.
func readSkillDetail(skillsDir, name string) (map[string]interface{}, error) {
	if !validSkillName(name) {
		return nil, fmt.Errorf("invalid skill name")
	}
	skillPath := filepath.Join(skillsDir, name)
	if !strings.HasPrefix(skillPath, skillsDir+string(filepath.Separator)) {
		return nil, fmt.Errorf("invalid skill name")
	}
	manifest, _ := skills.LoadManifest(skillsDir)
	entry, bundled := manifest.Skills[name]
	files := []map[string]string{}
	_ = filepath.Walk(skillPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if info.Name() == skills.BundledManifestFile {
			return nil
		}
		rel, err := filepath.Rel(skillPath, path)
		if err != nil {
			return nil
		}
		b, _ := os.ReadFile(path)
		files = append(files, map[string]string{"path": filepath.ToSlash(rel), "content": string(b)})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i]["path"] < files[j]["path"] })
	desc := ""
	for _, f := range files {
		if f["path"] == "SKILL.md" || f["path"] == "SKILL.md.disabled" {
			desc = skillSummaryMD(f["content"])
		}
	}
	enabled := false
	if _, err := os.Stat(filepath.Join(skillPath, "SKILL.md")); err == nil {
		enabled = true
	}
	return map[string]interface{}{
		"name":          name,
		"bundled":       bundled,
		"user_modified": bundled && entry.UserModified,
		"description":   desc,
		"optional":      skills.IsOptionalSkill(name),
		"enabled":       enabled,
		"files":         files,
	}, nil
}

// gitHubSkillTree lists blob paths under prefix for owner/repo on branch.
func gitHubSkillTree(owner, repo, branch, prefix string) ([]string, error) {
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
func installSkillFromGitHub(skillsDir, owner, repo, branch, prefix, destName string) error {
	if !validSkillName(destName) {
		return fmt.Errorf("invalid skill name")
	}
	paths, err := gitHubSkillTree(owner, repo, branch, prefix)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no skill files found at %s/%s", repo, prefix)
	}
	dest := filepath.Join(skillsDir, destName)
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

// localIP returns the machine's preferred outbound IPv4 address.
func localIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		if udp, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			return udp.IP.String()
		}
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ip4 := ipnet.IP.To4(); ip4 != nil {
					return ip4.String()
				}
			}
		}
	}
	return "127.0.0.1"
}

// systemUptime returns the system uptime as a short human-readable string,
// e.g. "11h 34m", matching the web console's format.
func systemUptime() string {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return time.Since(apiStartTime).String()
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return time.Since(apiStartTime).String()
	}
	uptimeSec, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return time.Since(apiStartTime).String()
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

// cpuUsagePercent samples /proc/stat twice and returns the CPU busy
// percentage between the two samples.
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

// memoryInfo returns used and total system memory in bytes.
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

// diskUsage returns used and total bytes for the given filesystem path.
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

// loadAverages reads the 1/5/15 minute load averages from /proc/loadavg.
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

func handleStats(w http.ResponseWriter, r *http.Request) {
	usedMem, totalMem := memoryInfo()
	usedDisk, totalDisk := diskUsage("/")
	one, five, fifteen := loadAverages()
	hostname, _ := os.Hostname()
	stats := map[string]interface{}{
		"version":     version,
		"uptime":      systemUptime(),
		"ip":          localIP(),
		"hostname":    hostname,
		"cpu_percent": cpuUsagePercent(),
		"cpu_count":   runtime.NumCPU(),
		"load":        map[string]float64{"one": one, "five": five, "fifteen": fifteen},
		"memory":      map[string]uint64{"used": usedMem, "total": totalMem},
		"disk":        map[string]uint64{"used": usedDisk, "total": totalDisk},
		"timestamp":   time.Now().Unix(),
	}
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
// Listens on the LAN (0.0.0.0) so paired mobile apps can connect over Wi-Fi.
// Loopback peers (web console proxy, relay client, TUI, CLI) are trusted;
// LAN peers must present valid per-device credentials on every request.
// One port (GHOST_API_PORT, default 8766) handles everything:
// chat, history, memory, transcription, remote control, and WebSocket.

// parseMemoryMeta extracts a human title, kind, summary, and source from
// the first chunk of a memory markdown file. Supports simple YAML-ish
// frontmatter delimited by --- lines, and falls back to the first H1
// heading if no frontmatter is present. Returns empty strings when a
// field is not present so callers can detect "unknown" without errors.
func parseMemoryMeta(head string) (title, kind, summary, source string) {
	// Frontmatter: ---\n key: value\n ---\n
	if strings.HasPrefix(strings.TrimLeft(head, "\n"), "---") {
		rest := strings.TrimLeft(head, "\n")
		rest = strings.TrimPrefix(rest, "---")
		// Find closing ---
		end := strings.Index(rest, "\n---")
		if end >= 0 {
			block := rest[:end]
			for _, line := range strings.Split(block, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				idx := strings.Index(line, ":")
				if idx <= 0 {
					continue
				}
				k := strings.TrimSpace(line[:idx])
				v := strings.TrimSpace(line[idx+1:])
				v = strings.Trim(v, "\"'`")
				switch strings.ToLower(k) {
				case "title":
					title = v
				case "kind":
					kind = strings.ToLower(v)
				case "summary":
					summary = v
				case "source":
					source = v
				}
			}
			// If no title in frontmatter, scan for first H1 after it.
			if title == "" {
				after := rest[end+4:]
				for _, line := range strings.Split(after, "\n") {
					line = strings.TrimSpace(line)
					if strings.HasPrefix(line, "# ") {
						title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
						break
					}
				}
			}
		}
	}
	// No frontmatter or no title yet — first H1 in head.
	if title == "" {
		for _, line := range strings.Split(head, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "# ") {
				title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
				break
			}
		}
	}
	// First paragraph as summary fallback.
	if summary == "" {
		inPara := false
		var b strings.Builder
		for _, line := range strings.Split(head, "\n") {
			trim := strings.TrimSpace(line)
			if trim == "" {
				if inPara {
					break
				}
				continue
			}
			if strings.HasPrefix(trim, "#") {
				continue
			}
			inPara = true
			if b.Len() > 0 {
				b.WriteString(" ")
			}
			b.WriteString(trim)
			if b.Len() > 140 {
				break
			}
		}
		summary = strings.TrimSpace(b.String())
		if len(summary) > 140 {
			summary = summary[:137] + "…"
		}
	}
	// Kind default.
	if kind == "" {
		kind = "fact"
	}
	return
}

func startInternalAPI(agentLoop *agent.AgentLoop, cronService *cron.CronService, scheduledService *scheduled.Service, channelManager *channels.Manager) {
	port := agentLoop.Config().Gateway.Port
	if p := os.Getenv("GHOST_API_PORT"); p != "" {
		fmt.Sscanf(p, "%d", &port)
	}

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
	workspaceDir := os.Getenv("GHOST_WORKSPACE_DIR")
	if workspaceDir == "" {
		home := os.Getenv("HOME")
		workspaceDir = filepath.Join(home, "ghost", "workspace")
	}
	skillsDir := filepath.Join(workspaceDir, "skills")

	db := agentLoop.DB()
	apiDB = db
	if channelManager != nil {
		channelManager.SetDeliveryObserver(func(msg bus.OutboundMessage, target string, ok bool, errText string) {
			// Now handled directly in Manager using telemetry.Global
		})
	}

	mux := http.NewServeMux()

	// ── 1. Health ─────────────────────────────────────────────────────────
	mux.HandleFunc("/v1/health", deviceAuthMiddleware(db, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "ok",
			"timestamp": time.Now().Unix(),
			"version":   "2.0.0",
			"uptime_s":  int64(time.Since(apiStartTime).Seconds()),
		})
	}))

	mux.HandleFunc("/v1/telemetry", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		system := map[string]interface{}{
			"memory_alloc_mb": float64(mem.Alloc) / 1024 / 1024,
			"num_goroutine":   runtime.NumGoroutine(),
		}

		fileCount := 0
		var sizeBytes int64 = 0
		if workspaceDir != "" {
			filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					fileCount++
					sizeBytes += info.Size()
				}
				return nil
			})
		}
		workspace := map[string]interface{}{
			"file_count": fileCount,
			"size_bytes": sizeBytes,
			"path":       workspaceDir,
		}

		database := map[string]interface{}{
			"active_sessions_24h": 0,
			"messages_24h":        0,
		}
		if db != nil {
			row := db.QueryRow(`
				SELECT COUNT(DISTINCT session_id), COUNT(*) 
				FROM messages 
				WHERE created_at > datetime('now', '-24 hours')`)
			var sessions, msgs int
			if err := row.Scan(&sessions, &msgs); err == nil {
				database["active_sessions_24h"] = sessions
				database["messages_24h"] = msgs
			}
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"system":    system,
			"workspace": workspace,
			"database":  database,
			"timestamp": time.Now().Unix(),
		})
	}))

	mux.HandleFunc("/v1/doctor", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
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
				Label:     check.Label,
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

		if channelManager != nil {
			statuses := channelManager.GetOperationalStatus()
			for name, raw := range statuses {
				statusMap, _ := raw.(map[string]interface{})
				failureCount, _ := statusMap["failure_count"].(int)
				lastErr, _ := statusMap["last_send_error"].(string)
				if failureCount >= 3 && lastErr != "" {
					level := "warning"
					if failureCount >= 5 {
						level = "error"
						overall = "error"
					} else if overall == "ok" {
						overall = "warning"
					}
					checks = append(checks, DoctorCheckPayload{
						Name:    "channel_" + name + "_delivery",
						Status:  level,
						Message: fmt.Sprintf("Repeated send failures (%d): %s", failureCount, lastErr),
					})
				}
			}
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    overall,
			"checks":    checks,
			"timestamp": time.Now().Unix(),
			"uptime":    int64(time.Since(apiStartTime).Seconds()),
			"version":   version,
			"profile": ProfileInfo{
				Name:        string(profileName),
				Permissions: permissions,
			},
			"channels": func() map[string]interface{} {
				if channelManager == nil {
					return map[string]interface{}{}
				}
				return channelManager.GetOperationalStatus()
			}(),
		})
	}))

	mux.HandleFunc("/v1/channels/status", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		if channelManager == nil {
			jsonResponse(w, http.StatusOK, map[string]interface{}{"channels": map[string]interface{}{}})
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"channels":  channelManager.GetOperationalStatus(),
			"timestamp": time.Now().Unix(),
		})
	}))

	mux.HandleFunc("/v1/channels/reconnect", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		if channelManager == nil {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "channel manager unavailable")
			return
		}
		var req struct {
			Channel string `json:"channel"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
			return
		}
		channel := strings.ToLower(strings.TrimSpace(req.Channel))
		if channel == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "channel is required")
			return
		}
		if err := channelManager.RestartChannel(r.Context(), channel); err != nil {
			jsonError(w, http.StatusBadRequest, "restart_failed", err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"ok":        true,
			"channel":   channel,
			"status":    channelManager.GetOperationalStatus(),
			"timestamp": time.Now().Unix(),
		})
	}))

	mux.HandleFunc("/v1/session/inspect", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		session := resolveSession(r)
		channel := strings.TrimSpace(r.URL.Query().Get("channel"))
		if channel == "" {
			channel = "mobile"
		}
		chatID := strings.TrimSpace(r.URL.Query().Get("chat_id"))
		if chatID == "" {
			chatID = "default"
		}
		lastChannel, lastChatID := agentLoop.GetLastActiveSession()
		target := channel
		if channelManager != nil {
			target = channelManager.ResolveTarget(bus.OutboundMessage{
				Channel: channel,
				ChatID:  chatID,
				Metadata: map[string]interface{}{
					"session_id": session,
				},
			})
		}
		lastReq := telemetry.Global.GetLastRequestID(session)
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"requested_session": session,
			"active_session": map[string]string{
				"channel": lastChannel,
				"chat_id": lastChatID,
			},
			"delivery_target": target,
			"last_request_id": lastReq,
			"timestamp":       time.Now().Unix(),
		})
	}))

	mux.HandleFunc("/v1/traces", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		requestID := strings.TrimSpace(r.URL.Query().Get("request_id"))
		session := strings.TrimSpace(r.URL.Query().Get("session"))
		if requestID == "" && session != "" {
			requestID = telemetry.Global.GetLastRequestID(session)
		}

		var traces []telemetry.TraceEvent
		if requestID != "" {
			traces = telemetry.Global.GetTraces(requestID)
		} else {
			// If no request/session specified, return the most recent global events
			traces = telemetry.Global.GetGlobalTraces(50)
		}
		incidents := telemetry.Global.GetIncidents()
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"request_id": requestID,
			"events":     traces,
			"incidents":  incidents,
		})
	}))

	// ── 1b. Cron lifecycle ───────────────────────────────────────────────
	mux.HandleFunc("/v1/cron/jobs/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
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
		if action == "" && r.Method == http.MethodDelete {
			if cronService.RemoveJob(jobID) {
				jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "id": jobID})
			} else {
				jsonError(w, http.StatusNotFound, "not_found", "job not found")
			}
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

	mux.HandleFunc("/v1/cron/jobs", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
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

		if r.Method == http.MethodPost {
			var req cronCreateRequestBody
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
				return
			}
			req.Name = strings.TrimSpace(req.Name)
			if req.Name == "" {
				jsonError(w, http.StatusBadRequest, "invalid_request", "name is required")
				return
			}
			switch req.Schedule.Kind {
			case "every", "cron", "at":
			case "":
				jsonError(w, http.StatusBadRequest, "invalid_request", "schedule.kind is required (every, cron, at)")
				return
			default:
				jsonError(w, http.StatusBadRequest, "invalid_request", "schedule.kind must be every, cron, or at")
				return
			}
			if req.Schedule.EveryMS != nil && *req.Schedule.EveryMS < 5000 {
				jsonError(w, http.StatusBadRequest, "invalid_request", "every interval must be at least 5 seconds")
				return
			}
			message := strings.TrimSpace(req.Message)
			command := strings.TrimSpace(req.Command)
			if message == "" && command == "" {
				jsonError(w, http.StatusBadRequest, "invalid_request", "message or command is required")
				return
			}
			if message == "" {
				message = command
			}
			job, err := cronService.AddJobWithOptions(
				req.Name, req.Schedule, message, req.Deliver,
				req.Channel, req.To, nil, req.Skills, req.NoAgent, "",
			)
			if err != nil {
				jsonError(w, http.StatusInternalServerError, "create_failed", err.Error())
				return
			}
			if command != "" && command != message {
				cmd := command
				if err := cronService.UpdateJob(job.ID, cron.JobUpdate{Command: &cmd}); err == nil {
					if j, ok := cronService.GetJob(job.ID); ok {
						job = j
					}
				}
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "job": job})
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

	// ── 1c. Scheduled items ──────────────────────────────────────────────
	mux.HandleFunc("/v1/scheduled", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if scheduledService == nil {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "scheduled service unavailable")
			return
		}

		switch r.Method {
		case http.MethodGet:
			// List scheduled items
			itemType := scheduled.ItemType(r.URL.Query().Get("type"))
			state := scheduled.ItemState(r.URL.Query().Get("state"))
			limit := 50
			if l := r.URL.Query().Get("limit"); l != "" {
				fmt.Sscanf(l, "%d", &limit)
			}
			items, err := scheduledService.ListItems(itemType, state, limit)
			if err != nil {
				jsonError(w, http.StatusInternalServerError, "list_failed", err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{
				"items": items,
			})

		case http.MethodPost:
			// Create new scheduled item
			var req struct {
				Type        string `json:"type"`
				Title       string `json:"title"`
				Description string `json:"description"`
				Schedule    struct {
					Kind  string  `json:"kind"`
					At    *string `json:"at"`
					Every *string `json:"every"`
					Expr  string  `json:"expr"`
					TZ    string  `json:"tz"`
				} `json:"schedule"`
				Action struct {
					Kind    string   `json:"kind"`
					Content string   `json:"content"`
					Command string   `json:"command"`
					Deliver bool     `json:"deliver"`
					Skills  []string `json:"skills"`
				} `json:"action"`
				Channel string `json:"channel"`
				ChatID  string `json:"chat_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
				return
			}
			req.Title = strings.TrimSpace(req.Title)
			if req.Title == "" {
				jsonError(w, http.StatusBadRequest, "invalid_request", "title is required")
				return
			}
			itemType := scheduled.ItemType(req.Type)
			if itemType == "" {
				itemType = scheduled.TypeAutomation
			}
			item := &scheduled.ScheduledItem{
				Type:         itemType,
				Title:        req.Title,
				Description:  req.Description,
				State:        scheduled.StateScheduled,
				Timezone:     "UTC",
				Channel:      req.Channel,
				ChatID:       req.ChatID,
				DeliveryMode: scheduled.DeliverySmart,
				Source:       "user",
				CreatedBy:    "api",
				MaxRetries:   3,
			}
			// Parse schedule
			switch req.Schedule.Kind {
			case "at":
				item.Schedule.Kind = scheduled.ScheduleAt
				if req.Schedule.At != nil {
					t, err := time.Parse(time.RFC3339, *req.Schedule.At)
					if err != nil {
						jsonError(w, http.StatusBadRequest, "invalid_request", "invalid at time")
						return
					}
					item.Schedule.At = &t
					item.NextRunAt = &t
				}
			case "every":
				item.Schedule.Kind = scheduled.ScheduleEvery
				if req.Schedule.Every != nil {
					d, err := time.ParseDuration(*req.Schedule.Every)
					if err != nil {
						jsonError(w, http.StatusBadRequest, "invalid_request", "invalid every duration")
						return
					}
					item.Schedule.Every = d
					next := time.Now().UTC().Add(d)
					item.NextRunAt = &next
				}
			case "cron":
				item.Schedule.Kind = scheduled.ScheduleCron
				item.Schedule.Expr = req.Schedule.Expr
				if req.Schedule.TZ != "" {
					item.Timezone = req.Schedule.TZ
				}
				// Compute next run from cron expression (simplified)
				next := time.Now().UTC().Add(time.Hour)
				item.NextRunAt = &next
			default:
				jsonError(w, http.StatusBadRequest, "invalid_request", "schedule.kind is required (at, every, cron)")
				return
			}
			// Parse action
			item.Action = scheduled.Action{
				Kind:    scheduled.ActionKind(req.Action.Kind),
				Content: req.Action.Content,
				Command: req.Action.Command,
				Deliver: req.Action.Deliver,
				Skills:  req.Action.Skills,
			}
			if item.Action.Kind == "" {
				item.Action.Kind = scheduled.ActionAgentTurn
			}
			if err := scheduledService.CreateItem(item); err != nil {
				jsonError(w, http.StatusInternalServerError, "create_failed", err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "item": item})

		default:
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	}))

	// Scheduled item by ID
	mux.HandleFunc("/v1/scheduled/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if scheduledService == nil {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "scheduled service unavailable")
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/v1/scheduled/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) < 1 || parts[0] == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "invalid item path")
			return
		}

		itemID := parts[0]
		action := ""
		if len(parts) > 1 {
			action = parts[1]
		}

		switch r.Method {
		case http.MethodGet:
			item, err := scheduledService.GetItem(itemID)
			if err != nil {
				jsonError(w, http.StatusNotFound, "not_found", "item not found")
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"item": item})

		case http.MethodPatch:
			// Update item
			var req struct {
				Title       *string `json:"title"`
				Description *string `json:"description"`
				State       *string `json:"state"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
				return
			}
			item, err := scheduledService.GetItem(itemID)
			if err != nil {
				jsonError(w, http.StatusNotFound, "not_found", "item not found")
				return
			}
			if req.Title != nil {
				item.Title = *req.Title
			}
			if req.Description != nil {
				item.Description = *req.Description
			}
			if req.State != nil {
				item.State = scheduled.ItemState(*req.State)
			}
			if err := scheduledService.UpdateItem(item); err != nil {
				jsonError(w, http.StatusInternalServerError, "update_failed", err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "item": item})

		case http.MethodDelete:
			if err := scheduledService.CancelItem(itemID); err != nil {
				jsonError(w, http.StatusNotFound, "not_found", "item not found")
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "id": itemID})

		default:
			// Handle actions like pause, resume, run
			if action != "" && r.Method == http.MethodPost {
				switch action {
				case "pause":
					if err := scheduledService.PauseItem(itemID); err != nil {
						jsonError(w, http.StatusNotFound, "not_found", err.Error())
						return
					}
					jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "state": "paused"})
				case "resume":
					if err := scheduledService.ResumeItem(itemID); err != nil {
						jsonError(w, http.StatusNotFound, "not_found", err.Error())
						return
					}
					jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "state": "scheduled"})
				case "run":
					if err := scheduledService.RunNow(itemID); err != nil {
						jsonError(w, http.StatusNotFound, "not_found", err.Error())
						return
					}
					jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "triggered"})
				default:
					jsonError(w, http.StatusBadRequest, "invalid_action", "unsupported action")
				}
				return
			}
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	}))

	// Scheduled execution history
	mux.HandleFunc("/v1/scheduled/history", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if scheduledService == nil {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "scheduled service unavailable")
			return
		}

		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}

		itemID := r.URL.Query().Get("item_id")
		if itemID == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "item_id is required")
			return
		}
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			fmt.Sscanf(l, "%d", &limit)
		}
		history, err := scheduledService.GetHistory(itemID, limit)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "history_failed", err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"history": history,
		})
	}))

	// ── 2. Chat (streaming SSE) ───────────────────────────────────────────
	mux.HandleFunc("/v1/chat", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
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
		if strings.TrimSpace(req.RequestID) == "" {
			req.RequestID = fmt.Sprintf("req-%d", time.Now().UnixNano())
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
		emitObject := func(payload interface{}) {
			raw, _ := json.Marshal(payload)
			fmt.Fprintf(w, "data: %s\n\n", string(raw))
			flusher.Flush()
		}
		emitObject(map[string]interface{}{
			"type":       "lifecycle",
			"request_id": req.RequestID,
			"state":      "queued",
		})

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
		emitObject(map[string]interface{}{
			"type":       "lifecycle",
			"request_id": req.RequestID,
			"state":      "agent_processing",
		})
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
			// agent_completed is recorded in AgentLoop
			meta := map[string]interface{}{
				"type":       "assistant_message",
				"request_id": req.RequestID,
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
			if _, ok := meta["session_id"]; !ok {
				meta["session_id"] = req.SessionKey
			}
			agentLoop.Bus().PublishOutbound(bus.OutboundMessage{
				Channel:  req.Channel,
				ChatID:   req.ChatID,
				Content:  response,
				Metadata: meta,
			})
			// channel_delivery is recorded in channels.Manager
			emitObject(map[string]interface{}{
				"type":       "lifecycle",
				"request_id": req.RequestID,
				"state":      "channel_delivery",
			})
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
		}
	}))

	// ── 3. History ────────────────────────────────────────────────────────
	mux.HandleFunc("/v1/history", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
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
			if !isUserVisibleHistoryMessage(m.Role, m.Content, metaJSON) {
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

		json.NewEncoder(w).Encode(map[string]interface{}{
			"messages": messages,
			"total":    total,
		})
	}))

	// ── 4. Search ─────────────────────────────────────────────────────────
	mux.HandleFunc("/v1/search", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/v1/tools", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		type toolEntry struct {
			Name string `json:"name"`
		}
		info := agentLoop.GetStartupInfo()
		// "tools" is {count:int, names:[]string} in startup info.
		var names []string
		if toolsInfo, ok := info["tools"].(map[string]interface{}); ok {
			if n, ok := toolsInfo["names"].([]string); ok {
				names = n
			}
		}
		toolsList := make([]toolEntry, 0, len(names))
		for _, name := range names {
			toolsList = append(toolsList, toolEntry{Name: name})
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"tools": toolsList,
		})
	}))

	// ── 4b. Skills ───────────────────────────────────────────────────────
	mux.HandleFunc("/v1/skills", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"ok":     true,
			"skills": listWorkspaceSkills(skillsDir),
		})
	}))

	mux.HandleFunc("/v1/skills/toggle", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		var req struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "name is required")
			return
		}
		if err := setSkillEnabled(skillsDir, req.Name, req.Enabled); err != nil {
			jsonError(w, http.StatusBadRequest, "toggle_failed", err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "name": req.Name, "enabled": req.Enabled})
	}))

	mux.HandleFunc("/v1/skills/read", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		name := r.URL.Query().Get("name")
		detail, err := readSkillDetail(skillsDir, name)
		if err != nil {
			jsonError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		detail["ok"] = true
		jsonResponse(w, http.StatusOK, detail)
	}))

	mux.HandleFunc("/v1/skills/install", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
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
			jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
			return
		}
		if req.Owner == "" || req.Repo == "" || req.Path == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "owner, repo and path are required")
			return
		}
		if req.Name == "" {
			req.Name = filepath.Base(strings.TrimSuffix(req.Path, "/"))
		}
		if req.Branch == "" {
			req.Branch = "main"
		}
		if err := installSkillFromGitHub(skillsDir, req.Owner, req.Repo, req.Branch, req.Path, req.Name); err != nil {
			jsonError(w, http.StatusInternalServerError, "install_failed", err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "name": req.Name, "message": "Skill installed"})
	}))

	// ── 5. Memory files ───────────────────────────────────────────────────
	mux.HandleFunc("/v1/memory/files", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		type FileInfo struct {
			Name     string `json:"name"`
			Modified int64  `json:"modified"`
			Size     int64  `json:"size"`
			Title    string `json:"title"`
			Kind     string `json:"kind"`
			Summary  string `json:"summary"`
			Source   string `json:"source"`
		}
		var files []FileInfo

		// Optional query: filter by file name OR note content, so "search
		// memories" can find a note by what it says, not just its name.
		q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))

		if _, err := os.Stat(memoryDir); err != nil {
			json.NewEncoder(w).Encode([]FileInfo{})
			return
		}

		filepath.Walk(memoryDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(info.Name(), ".md") {
				return nil
			}
			rel, _ := filepath.Rel(memoryDir, path)
			entry := FileInfo{
				Name:     rel,
				Modified: info.ModTime().Unix(),
				Size:     info.Size(),
			}
			// Read just the first 4KB to extract metadata without scanning
			// the whole file for every entry.
			if f, err := os.Open(path); err == nil {
				defer f.Close()
				buf := make([]byte, 4096)
				n, _ := f.Read(buf)
				entry.Title, entry.Kind, entry.Summary, entry.Source = parseMemoryMeta(string(buf[:n]))
			}
			// When searching, match against the file name or its content.
			if q != "" {
				content := ""
				if b, err := os.ReadFile(path); err == nil {
					content = string(b)
				}
				if !strings.Contains(strings.ToLower(entry.Name), q) && !strings.Contains(strings.ToLower(content), q) && !strings.Contains(strings.ToLower(entry.Title), q) {
					return nil
				}
			}
			files = append(files, entry)
			return nil
		})
		if files == nil {
			files = []FileInfo{}
		}
		json.NewEncoder(w).Encode(files)
	}))

	// ── 6. Memory file content ────────────────────────────────────────────
	mux.HandleFunc("/v1/memory/file", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
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
		full := filepath.Join(memoryDir, clean)
		if r.Method == http.MethodDelete {
			if err := os.Remove(full); err != nil {
				if os.IsNotExist(err) {
					jsonError(w, http.StatusNotFound, "not_found", "memory not found")
					return
				}
				jsonError(w, http.StatusInternalServerError, "io_error", "could not forget memory")
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
			return
		}
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		content, err := os.ReadFile(full)
		if err != nil {
			jsonError(w, http.StatusNotFound, "not_found", "file not found")
			return
		}
		jsonResponse(w, http.StatusOK, map[string]string{"content": string(content)})
	}))

	// ── 6b. Curated personal state ("what Ghost knows about you") ─────────
	// Surfaces the structured personal-context store (auto-extracted from
	// conversation) plus the curated-memory notes (the model's own notes) so a
	// user can see and manage what Ghost has learned about them. The personal
	// context is always injected into the system prompt (consult + apply); this
	// makes it visible and editable.
	// ── Recall: cross-session "what did we talk about earlier?" ────────────
	// Surfaces Ghost's own session history: raw matches offline, or a concise
	// LLM summary when a cloud model is available.
	mux.HandleFunc("/v1/recall", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("query"))
		if q == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "query is required")
			return
		}
		jsonResponse(w, http.StatusOK, agentLoop.Recall(r.Context(), q))
	}))

	// ── Learnings: a readable digest of Ghost's self-improvement ───────────
	mux.HandleFunc("/v1/learnings", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, agentLoop.LearningsSummary())
	}))

	mux.HandleFunc("/v1/memory/self", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		type entryView struct {
			ID             string     `json:"id"`
			Kind           string     `json:"kind"`
			Label          string     `json:"label"`
			Title          string     `json:"title"`
			Summary        string     `json:"summary"`
			Domain         string     `json:"domain"`
			DomainLabel    string     `json:"domain_label"`
			Value          string     `json:"value"`
			CreatedAt      time.Time  `json:"created_at,omitempty"`
			ReinforceCount int        `json:"reinforce_count,omitempty"`
			ReinforcedAt   *time.Time `json:"reinforced_at,omitempty"`
		}
		store, err := personalcontext.Open(workspaceDir)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "io_error", "could not read saved facts")
			return
		}
		entries := make([]entryView, 0)
		for _, e := range store.Current() {
			domain := personalcontext.ClassifyEntryDomain(e)
			entries = append(entries, entryView{
				ID:             e.ID,
				Kind:           string(e.Kind),
				Label:          personalcontext.Label(e.Predicate),
				Title:          personalcontext.Title(e),
				Summary:        personalcontext.Summary(e),
				Domain:         string(domain),
				DomainLabel:    personalcontext.DomainLabel(domain),
				Value:          personalcontext.Value(e),
				CreatedAt:      e.CreatedAt,
				ReinforceCount: e.ReinforceCount,
				ReinforcedAt:   e.ReinforcedAt,
			})
		}
		curate := tools.NewMemoryCurateTool(workspaceDir)
		notes := curate.Entries("memory")
		you := curate.Entries("user")
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"entries": entries,
			"notes":   notes,
			"you":     you,
		})
	}))

	mux.HandleFunc("/v1/memory/self/forget", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		var req struct {
			ID     string `json:"id"`     // auto-extracted personal-context entry
			Target string `json:"target"` // "user" or "memory" for curated notes
			Entry  string `json:"entry"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "invalid_request", "invalid request")
			return
		}
		if req.ID != "" {
			store, err := personalcontext.Open(workspaceDir)
			if err != nil {
				jsonError(w, http.StatusInternalServerError, "io_error", "could not read saved facts")
				return
			}
			if err := store.Forget(req.ID); err != nil {
				jsonError(w, http.StatusNotFound, "not_found", "that fact wasn't found")
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
			return
		}
		curate := tools.NewMemoryCurateTool(workspaceDir)
		count, err := curate.Delete(req.Target, req.Entry)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "not_found", err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "remaining": count})
	}))

	mux.HandleFunc("/v1/workspace/files", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		type FileInfo struct {
			Name     string `json:"name"`
			Modified int64  `json:"modified"`
			Size     int64  `json:"size"`
		}
		var files []FileInfo

		if _, err := os.Stat(workspaceDir); err != nil {
			json.NewEncoder(w).Encode([]FileInfo{})
			return
		}

		filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(workspaceDir, path)
			if err != nil {
				return nil
			}
			files = append(files, FileInfo{
				Name:     rel,
				Modified: info.ModTime().Unix(),
				Size:     info.Size(),
			})
			return nil
		})
		if files == nil {
			files = []FileInfo{}
		}
		json.NewEncoder(w).Encode(files)
	}))

	mux.HandleFunc("/v1/workspace/file", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
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

		fullPath := filepath.Join(workspaceDir, clean)
		rel, err := filepath.Rel(workspaceDir, fullPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			jsonError(w, http.StatusForbidden, "forbidden", "invalid path")
			return
		}

		info, err := os.Stat(fullPath)
		if err != nil {
			jsonError(w, http.StatusNotFound, "not_found", "file not found")
			return
		}
		if info.IsDir() {
			jsonError(w, http.StatusBadRequest, "invalid_request", "path is a directory")
			return
		}

		const maxPreviewBytes int64 = 256 * 1024
		data, err := os.ReadFile(fullPath)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "io_error", "failed to read file")
			return
		}

		mimeType := ""
		if ext := strings.ToLower(filepath.Ext(fullPath)); ext != "" {
			mimeType = mime.TypeByExtension(ext)
		}
		if len(data) > 0 {
			sniffLen := 512
			if len(data) < sniffLen {
				sniffLen = len(data)
			}
			detected := http.DetectContentType(data[:sniffLen])
			if mimeType == "" || strings.HasPrefix(detected, "image/") {
				mimeType = detected
			}
		}
		if strings.HasPrefix(mimeType, "image/") {
			const maxImagePreviewBytes int64 = 4 * 1024 * 1024
			if int64(len(data)) > maxImagePreviewBytes {
				jsonResponse(w, http.StatusOK, map[string]interface{}{
					"previewable": false,
					"kind":        "image",
					"mime_type":   mimeType,
					"reason":      "image_too_large",
					"size":        info.Size(),
					"truncated":   true,
					"content":     "",
				})
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{
				"previewable":  true,
				"kind":         "image",
				"mime_type":    mimeType,
				"reason":       "",
				"size":         info.Size(),
				"truncated":    false,
				"content":      "",
				"image_base64": base64.StdEncoding.EncodeToString(data),
			})
			return
		}

		preview := data
		truncated := false
		if int64(len(data)) > maxPreviewBytes {
			preview = data[:maxPreviewBytes]
			truncated = true
		}

		if bytes.Contains(preview, []byte{0x00}) || !utf8.Valid(preview) {
			jsonResponse(w, http.StatusOK, map[string]interface{}{
				"previewable": false,
				"kind":        "binary",
				"mime_type":   mimeType,
				"reason":      "binary_or_unsupported_encoding",
				"size":        info.Size(),
				"truncated":   truncated,
				"content":     "",
			})
			return
		}

		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"previewable": true,
			"kind":        "text",
			"mime_type":   mimeType,
			"reason":      "",
			"size":        info.Size(),
			"truncated":   truncated,
			"content":     string(preview),
		})
	}))

	// ── 7. Transcribe ─────────────────────────────────────────────────────
	mux.HandleFunc("/v1/transcribe", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/v1/upload", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/v1/message", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/v1/messages", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
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

	// ── 11. Delete session ────────────────────────────────────────────────
	mux.HandleFunc("/v1/session", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", 405)
			return
		}
		session := r.URL.Query().Get("id")
		if session == "" {
			http.Error(w, "id required", 400)
			return
		}
		if db != nil {
			// Permanently delete all messages for this session
			_, err := db.Exec("DELETE FROM messages WHERE session_id = ?", session)
			if err != nil {
				jsonError(w, http.StatusInternalServerError, "db_error", err.Error())
				return
			}
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))

	// ── 12. Rename session ────────────────────────────────────────────────
	mux.HandleFunc("/v1/session/rename", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}

		var req struct {
			OldID string `json:"old_id"`
			NewID string `json:"new_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
			return
		}
		oldID := strings.TrimSpace(req.OldID)
		newID := strings.TrimSpace(req.NewID)
		if oldID == "" || newID == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "old_id and new_id are required")
			return
		}
		if oldID == newID {
			json.NewEncoder(w).Encode(map[string]bool{"ok": true})
			return
		}
		if db == nil {
			jsonError(w, http.StatusInternalServerError, "internal_error", "database not available")
			return
		}

		tx, err := db.Begin()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer tx.Rollback()

		var existing int
		if err := tx.QueryRow("SELECT COUNT(1) FROM messages WHERE session_id = ?", newID).Scan(&existing); err != nil {
			jsonError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if existing > 0 {
			jsonError(w, http.StatusConflict, "conflict", "target session id already exists")
			return
		}

		res, err := tx.Exec("UPDATE messages SET session_id = ? WHERE session_id = ?", newID, oldID)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if err := tx.Commit(); err != nil {
			jsonError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		rowsAffected, _ := res.RowsAffected()
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"ok":            true,
			"rows_affected": rowsAffected,
		})
	}))

	// ── Remote control endpoints ──────────────────────────────────────────
	mux.HandleFunc("/v1/exec", authMiddleware(handleExec(allowedCmds)))
	mux.HandleFunc("/v1/screenshot", authMiddleware(handleScreenshot(screenshotCmd)))
	mux.HandleFunc("/v1/stats", authMiddleware(handleStats))
	mux.HandleFunc("/v1/open", authMiddleware(handleOpen))

	// ── Mid-turn steering ─────────────────────────────────────────────────
	mux.HandleFunc("/v1/steering", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			SessionKey string `json:"session_key"`
			Content    string `json:"content"`
			Action     string `json:"action"` // "redirect" | "interrupt" | "abort"
			Channel    string `json:"channel"`
			ChatID     string `json:"chat_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"ok":false,"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		sm := agentLoop.Steering()
		if sm == nil {
			http.Error(w, `{"ok":false,"error":"steering unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		switch req.Action {
		case "abort":
			sm.HardAbort(req.SessionKey)
		case "interrupt":
			sm.Interrupt(req.SessionKey)
		default: // "redirect"
			if req.Content == "" {
				http.Error(w, `{"ok":false,"error":"content required for redirect"}`, http.StatusBadRequest)
				return
			}
			sm.Inject(agent.SteeringMessage{
				Content:    req.Content,
				SessionKey: req.SessionKey,
				Channel:    req.Channel,
				ChatID:     req.ChatID,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))

	// ── Clarify responses ─────────────────────────────────────────────────
	// The clarify tool blocks the agent turn waiting for the user's answer.
	// The mobile app renders the clarify_request as an interactive card and
	// posts the answer here.
	mux.HandleFunc("/v1/clarify/respond", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		var req struct {
			QuestionID string `json:"question_id"`
			Response   string `json:"response"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
			return
		}
		req.QuestionID = strings.TrimSpace(req.QuestionID)
		req.Response = strings.TrimSpace(req.Response)
		if req.QuestionID == "" || req.Response == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "question_id and response are required")
			return
		}
		registry := agentLoop.Tools()
		if registry == nil {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "tool registry unavailable")
			return
		}
		tool, ok := registry.Get("clarify")
		if !ok {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "clarify tool unavailable")
			return
		}
		ct, ok := tool.(*tools.ClarifyTool)
		if !ok {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "clarify tool unavailable")
			return
		}
		if !ct.HandleResponse(req.QuestionID, req.Response) {
			jsonError(w, http.StatusNotFound, "not_found", "question not found, already answered, or timed out")
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
	}))

	// ── Model presets / live switching ────────────────────────────────────
	mux.HandleFunc("/v1/model", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			type presetPayload struct {
				Name     string `json:"name"`
				Provider string `json:"provider"`
				Model    string `json:"model"`
			}
			presets := []presetPayload{}
			if cfg := agentLoop.Config(); cfg != nil {
				for _, p := range cfg.Agents.ModelList {
					if p.Name == "" {
						continue
					}
					presets = append(presets, presetPayload{Name: p.Name, Provider: p.Provider, Model: p.Model})
				}
			}
			provider := ""
			if cfg := agentLoop.Config(); cfg != nil {
				provider = cfg.Agents.Defaults.Provider
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{
				"active":   agentLoop.GetCurrentModel(),
				"provider": provider,
				"presets":  presets,
			})
		case http.MethodPost:
			var req struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
				return
			}
			req.Model = strings.TrimSpace(req.Model)
			if req.Model == "" {
				jsonError(w, http.StatusBadRequest, "invalid_request", "model is required (preset name or provider:model)")
				return
			}
			if err := agentLoop.SetModel(req.Model); err != nil {
				jsonError(w, http.StatusBadRequest, "switch_failed", err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{
				"ok":     true,
				"active": agentLoop.GetCurrentModel(),
			})
		default:
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	}))

	// ── Session list ──────────────────────────────────────────────────────
	mux.HandleFunc("/v1/sessions", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		if db == nil {
			jsonError(w, http.StatusInternalServerError, "internal_error", "database not available")
			return
		}
		rows, err := db.Query(`
			SELECT m1.session_id,
			       COUNT(*),
			       COALESCE(unixepoch(MAX(m1.created_at)), 0),
			       COALESCE(s.title, (
			           SELECT m2.content FROM messages m2
			           WHERE m2.session_id = m1.session_id
			             AND m2.role = 'user'
			             AND (m2.archived IS NULL OR m2.archived = 0)
			             AND TRIM(COALESCE(m2.content, '')) != ''
			           ORDER BY datetime(m2.created_at) ASC, m2.rowid ASC
			           LIMIT 1
			       ), '') AS title
			FROM messages m1
			LEFT JOIN sessions s ON s.id = m1.session_id
			WHERE (m1.archived IS NULL OR m1.archived = 0)
			  AND m1.session_id != 'heartbeat'
			GROUP BY m1.session_id
			ORDER BY MAX(m1.created_at) DESC
			LIMIT 100`)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		type sessionEntry struct {
			ID           string `json:"id"`
			Title        string `json:"title"`
			MessageCount int    `json:"message_count"`
			LastActivity int64  `json:"last_activity"`
		}
		sessions := []sessionEntry{}
		for rows.Next() {
			var e sessionEntry
			if err := rows.Scan(&e.ID, &e.MessageCount, &e.LastActivity, &e.Title); err != nil {
				continue
			}
			if len(e.Title) > 80 {
				e.Title = e.Title[:80]
			}
			sessions = append(sessions, e)
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"sessions": sessions,
		})
	}))

	// ── Pairing ────────────────────────────────────────────────────────────
	// Cleanup expired tokens on startup (best-effort).
	if db != nil {
		_ = pairing.CleanupExpired(db)
	}

	// Helper to return structured pairing errors.
	pairingErrorResponse := func(w http.ResponseWriter, status int, err error) {
		if pe, ok := err.(*pairing.PairingError); ok {
			jsonResponse(w, status, map[string]interface{}{
				"error": map[string]string{
					"code":    pe.Code,
					"message": pe.Message,
				},
			})
			return
		}
		jsonError(w, status, "internal_error", err.Error())
	}

	// POST /v1/pairing/invitations — generate a pairing invitation.
	// Called by Ghost Pod web UI when user wants to pair a phone.
	mux.HandleFunc("/v1/pairing/invitations", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		if db == nil {
			jsonError(w, http.StatusInternalServerError, "internal_error", "database not available")
			return
		}

		// Get pod identity for the invitation.
		workspace := agentLoop.Config().Agents.Defaults.Workspace
		podID, _ := ghoststate.GetPodID(workspace)

		var req struct {
			DisplayName string `json:"display_name"`
			Transport   string `json:"transport"`
			Host        string `json:"host"`
			Port        string `json:"port"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			req = struct {
				DisplayName string `json:"display_name"`
				Transport   string `json:"transport"`
				Host        string `json:"host"`
				Port        string `json:"port"`
			}{DisplayName: "Phone", Transport: "lan", Host: detectLANAddress(), Port: "8766"}
		}
		if req.DisplayName == "" {
			req.DisplayName = "Phone"
		}
		if req.Transport == "" {
			req.Transport = "lan"
		}
		if req.Host == "" {
			req.Host = detectLANAddress()
		}
		if req.Port == "" {
			req.Port = fmt.Sprintf("%d", agentLoop.Config().Gateway.Port)
		}

		invitation, err := pairing.CreatePairingInvitation(db, podID, req.Transport, req.Host, req.Port, req.DisplayName)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "pairing_error", err.Error())
			return
		}

		jsonResponse(w, http.StatusOK, invitation)
	}))

	// Legacy: POST /v1/pairing/start — kept for backward compatibility.
	mux.HandleFunc("/v1/pairing/start", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		if db == nil {
			jsonError(w, http.StatusInternalServerError, "internal_error", "database not available")
			return
		}
		workspace := agentLoop.Config().Agents.Defaults.Workspace
		podID, _ := ghoststate.GetPodID(workspace)
		port := fmt.Sprintf("%d", agentLoop.Config().Gateway.Port)

		var req struct {
			DisplayName string `json:"display_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DisplayName == "" {
			req.DisplayName = "Phone"
		}

		invitation, err := pairing.CreatePairingInvitation(db, podID, "lan", detectLANAddress(), port, req.DisplayName)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "pairing_error", err.Error())
			return
		}

		jsonResponse(w, http.StatusOK, invitation)
	}))

	// POST /v1/pairing/complete — mobile app presents token + device metadata, gets credentials.
	// Single-use. Token expires after 5 minutes.
	// PUBLIC endpoint — no auth required (the token itself is the authorization).
	mux.HandleFunc("/v1/pairing/complete", publicHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		if db == nil {
			jsonError(w, http.StatusInternalServerError, "internal_error", "database not available")
			return
		}
		var req struct {
			Token       string `json:"token"`
			DisplayName string `json:"display_name"`
			Platform    string `json:"platform"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "token required")
			return
		}

		result, err := pairing.RedeemPairing(db, req.Token, req.DisplayName, req.Platform)
		if err != nil {
			pairingErrorResponse(w, http.StatusUnauthorized, err)
			return
		}

		// Include Ghost name in response.
		workspace := agentLoop.Config().Agents.Defaults.Workspace
		if id, err := ghoststate.LoadIdentity(workspace); err == nil && id != nil {
			result.GhostName = id.GhostName
		}

		jsonResponse(w, http.StatusOK, result)
	}))

	// Legacy: POST /v1/pairing/redeem — kept for backward compatibility.
	mux.HandleFunc("/v1/pairing/redeem", publicHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		if db == nil {
			jsonError(w, http.StatusInternalServerError, "internal_error", "database not available")
			return
		}
		var req struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "token required")
			return
		}

		result, err := pairing.RedeemPairing(db, req.Token, "Phone", "unknown")
		if err != nil {
			pairingErrorResponse(w, http.StatusUnauthorized, err)
			return
		}

		jsonResponse(w, http.StatusOK, result)
	}))

	// POST /v1/pairing/revoke — revoke a paired device.
	mux.HandleFunc("/v1/pairing/revoke", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		if db == nil {
			jsonError(w, http.StatusInternalServerError, "internal_error", "database not available")
			return
		}
		var req struct {
			DeviceID string `json:"device_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DeviceID == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "device_id required")
			return
		}

		if err := pairing.RevokeDevice(db, req.DeviceID); err != nil {
			jsonError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}

		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
	}))

	// GET /v1/pairing/devices — list all paired devices.
	mux.HandleFunc("/v1/pairing/devices", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		if db == nil {
			jsonError(w, http.StatusInternalServerError, "internal_error", "database not available")
			return
		}

		devices, err := pairing.ListDevices(db)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if devices == nil {
			devices = []pairing.PairedDevice{}
		}

		// Enrich with capabilities. Ghost Mobile exposes chat, memory and
		// voice today; the per-device capability list will be tightened once
		// the mobile app negotiates scopes at pairing time.
		type deviceView struct {
			pairing.PairedDevice
			Capabilities []string `json:"capabilities"`
		}
		views := make([]deviceView, 0, len(devices))
		for _, d := range devices {
			views = append(views, deviceView{
				PairedDevice: d,
				Capabilities: []string{"chat", "memory", "voice"},
			})
		}

		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"devices": views,
		})
	}))

	// POST /v1/pairing/cancel — discard a pending pairing token.
	mux.HandleFunc("/v1/pairing/cancel", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		if db == nil {
			jsonError(w, http.StatusInternalServerError, "internal_error", "database not available")
			return
		}
		var req struct {
			PairingID string `json:"pairing_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PairingID == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "pairing_id required")
			return
		}

		_, err := db.Exec(`DELETE FROM pending_pairings WHERE id = ?`, req.PairingID)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
	}))

	// ── WebSocket ─────────────────────────────────────────────────────────
	mux.HandleFunc("/v1/ws", handleWebSocket(agentLoop))

	addr := fmt.Sprintf("0.0.0.0:%d", port)
	log.Printf("🤖 Ghost Internal API listening on %s (chat + tools; loopback trusted, LAN requires device credentials)", addr)

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
