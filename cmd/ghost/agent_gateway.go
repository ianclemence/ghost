package main

// Ghost agent ↔ gateway client.
//
// Option 2 ("one conversation"): when the local gateway daemon is
// reachable, the terminal chat stops running its own embedded AgentLoop and
// becomes a thin client of the gateway — the same HTTP+SSE surface the
// mobile app uses. Same session store, same permission broker, same turn
// lifecycle, live rather than polled. When the daemon is down, the CLI
// falls back to the embedded loop (today's behavior) so offline chat keeps
// working.
//
// The client implements agentRuntime, so the TUI is transport-blind: it
// cannot tell whether a turn ran in-process or on the daemon.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/providers"
)

// embeddedRuntime adapts the in-process AgentLoop to agentRuntime, adding the
// history backfill the TUI needs to switch threads. All other methods come
// from the embedded loop.
type embeddedRuntime struct{ *agent.AgentLoop }

// LoadHistory converts the embedded loop's stored messages into transcript
// entries, mirroring the gateway client's shape.
func (e embeddedRuntime) LoadHistory(sessionKey string) ([]historyEntry, error) {
	msgs := e.AgentLoop.History(sessionKey)
	out := make([]historyEntry, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "user", "assistant":
			out = append(out, historyEntry{Role: m.Role, Content: m.Content})
		}
	}
	return out, nil
}

// gatewayRuntime is agentRuntime over HTTP+SSE to a local ghost gateway.
// All requests target loopback, which the gateway trusts without device
// credentials; nothing here ever leaves the machine.
type gatewayRuntime struct {
	baseURL string
	http    *http.Client

	// Model and context reads back the footer on every frame. Cache them
	// briefly so a stalled daemon can never freeze rendering; writes
	// bust the cache so picks apply instantly.
	mu           sync.Mutex
	modelActive  string
	modelPresets []string
	modelOptions []providers.ModelOption
	modelAll     []providers.ModelOption
	modelAt      time.Time
	ctxCurrent   map[string]string
	ctxList      []string
	ctxAt        time.Time

	// scoped is the enabled/ordered cycling set. The daemon owns the durable
	// copy (kv_store); the gateway mirrors it for the session.
	scoped providers.ScopedModels
}

const gatewayCacheTTL = 5 * time.Second

// gatewayBaseURL resolves the gateway address from the config gateway port,
// overridden by GHOST_API_PORT.
func gatewayBaseURL(cfg *config.Config) string {
	port := cfg.Gateway.Port
	if p := os.Getenv("GHOST_API_PORT"); p != "" {
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err == nil && n > 0 {
			port = n
		}
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// gatewayReachable reports whether a gateway answers on baseURL. Short
// timeout: this runs on every CLI start and must never hang it.
func gatewayReachable(baseURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/v1/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// gatewayErrorEnvelope decodes the gateway's {error:{kind,message}} shape.
func gatewayErrorMessage(body []byte) string {
	var env struct {
		Error struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return ""
	}
	if env.Error.Message != "" {
		return env.Error.Message
	}
	return env.Error.Kind
}

func (g *gatewayRuntime) post(ctx context.Context, path string, body interface{}, sessionKey string) ([]byte, int, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Type", "cli")
	if sessionKey != "" {
		req.Header.Set("X-Ghost-Session", sessionKey)
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if msg := gatewayErrorMessage(out); msg != "" {
			return nil, resp.StatusCode, fmt.Errorf("%s", msg)
		}
		return nil, resp.StatusCode, fmt.Errorf("gateway %s answered %d", path, resp.StatusCode)
	}
	return out, resp.StatusCode, nil
}

func (g *gatewayRuntime) getJSON(ctx context.Context, path string, sessionKey string, target interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Client-Type", "cli")
	if sessionKey != "" {
		req.Header.Set("X-Ghost-Session", sessionKey)
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if msg := gatewayErrorMessage(out); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return fmt.Errorf("gateway %s answered %d", path, resp.StatusCode)
	}
	return json.Unmarshal(out, target)
}

// ─── agentRuntime ────────────────────────────────────────────────────────

func (g *gatewayRuntime) GetCurrentModel() string {
	g.mu.Lock()
	if time.Since(g.modelAt) < gatewayCacheTTL && g.modelActive != "" {
		defer g.mu.Unlock()
		return g.modelActive
	}
	g.mu.Unlock()
	var res struct {
		Active string `json:"active"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := g.getJSON(ctx, "/v1/model", "", &res); err != nil || res.Active == "" {
		return "unknown"
	}
	g.mu.Lock()
	g.modelActive = res.Active
	g.modelAt = time.Now()
	g.mu.Unlock()
	return res.Active
}

func (g *gatewayRuntime) ModelPresets() []string {
	names, _ := g.modelCatalog()
	return names
}

// ModelOptions returns the full switchable set the daemon reports
// (presets + connections + keyed providers), falling back to bare preset
// names when the daemon predates the options field.
func (g *gatewayRuntime) ModelOptions() []providers.ModelOption {
	_, opts := g.modelCatalog()
	return opts
}

// modelCatalog fetches and caches the daemon's model listing once per
// TTL: preset names plus the full option set when available.
func (g *gatewayRuntime) modelCatalog() ([]string, []providers.ModelOption) {
	g.mu.Lock()
	if time.Since(g.modelAt) < gatewayCacheTTL && g.modelPresets != nil {
		defer g.mu.Unlock()
		return append([]string{}, g.modelPresets...), append([]providers.ModelOption{}, g.modelOptions...)
	}
	g.mu.Unlock()
	var res struct {
		Active  string `json:"active"`
		Presets []struct {
			Name string `json:"name"`
		} `json:"presets"`
		Options    []providers.ModelOption `json:"options"`
		AllOptions []providers.ModelOption `json:"all_options"`
		Scope      []string                `json:"scope"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := g.getJSON(ctx, "/v1/model", "", &res); err != nil {
		return nil, nil
	}
	names := make([]string, 0, len(res.Presets))
	for _, p := range res.Presets {
		if p.Name != "" {
			names = append(names, p.Name)
		}
	}
	all := res.AllOptions
	if len(all) == 0 {
		all = res.Options // older daemon: fall back to the switchable set
	}
	g.mu.Lock()
	if res.Active != "" {
		g.modelActive = res.Active
	}
	g.modelPresets = append([]string{}, names...)
	g.modelOptions = append([]providers.ModelOption{}, res.Options...)
	g.modelAll = append([]providers.ModelOption{}, all...)
	g.scoped.Set(res.Scope)
	g.modelAt = time.Now()
	g.mu.Unlock()
	return names, append([]providers.ModelOption{}, res.Options...)
}

// AllModelOptions returns the full catalog the daemon reported, falling back
// to the switchable set on an older daemon.
func (g *gatewayRuntime) AllModelOptions() []providers.ModelOption {
	g.modelCatalog()
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.modelAll) > 0 {
		return append([]providers.ModelOption{}, g.modelAll...)
	}
	return append([]providers.ModelOption{}, g.modelOptions...)
}

// GetScopedModels returns the cycling set the daemon reported (cached with
// the model catalog).
func (g *gatewayRuntime) GetScopedModels() providers.ScopedModels {
	g.modelCatalog()
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.scoped
}

// SetScopedModels persists the cycling set on the daemon (the durable copy)
// and mirrors it locally. A nil slice clears the scope back to all-enabled.
func (g *gatewayRuntime) SetScopedModels(ids []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	body := map[string]interface{}{}
	if ids != nil {
		body["scope"] = ids
	} else {
		body["all_enabled"] = true
	}
	if _, _, err := g.post(ctx, "/v1/model", body, ""); err != nil {
		return err
	}
	g.mu.Lock()
	g.scoped.Set(ids)
	g.mu.Unlock()
	return nil
}

// RefreshModels busts the cached model state so the picker always opens
// on live data.
func (g *gatewayRuntime) RefreshModels() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.modelAt = time.Time{}
	g.modelPresets = nil
	g.modelOptions = nil
	g.modelAll = nil
}

func (g *gatewayRuntime) SetModel(target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, _, err := g.post(ctx, "/v1/model", map[string]string{"model": target}, "")
	if err != nil {
		return err
	}
	var res struct {
		Active string `json:"active"`
	}
	_ = json.Unmarshal(out, &res)
	g.mu.Lock()
	if res.Active != "" {
		g.modelActive = res.Active
		g.modelAt = time.Now()
	}
	g.mu.Unlock()
	return nil
}

// InjectSteering queues a follow-up into the daemon's running turn — the
// remote twin of AgentLoop.InjectSteering (same endpoint the app uses).
func (g *gatewayRuntime) InjectSteering(sessionKey, content string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _, _ = g.post(ctx, "/v1/steering", map[string]string{
		"session_key": sessionKey,
		"content":     content,
		"action":      "redirect",
		"channel":     "cli",
		"chat_id":     "direct",
	}, sessionKey)
}

// AbortTurn aborts the daemon's running turn — the remote twin of
// AgentLoop.AbortTurn (the Esc path).
func (g *gatewayRuntime) AbortTurn(sessionKey string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _, _ = g.post(ctx, "/v1/steering", map[string]string{
		"session_key": sessionKey,
		"action":      "abort",
	}, sessionKey)
}

// RespondClarify answers an in-flight clarification on the daemon — the
// remote twin of AgentLoop.RespondClarify.
func (g *gatewayRuntime) RespondClarify(questionID, response string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, _, err := g.post(ctx, "/v1/clarify/respond", map[string]string{
		"question_id": questionID,
		"response":    response,
	}, "")
	if err != nil {
		return false
	}
	var res struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return false
	}
	return res.OK
}

// PendingApproval reports the daemon broker's pending request for this
// session, if any. It mirrors AgentLoop.PendingApproval: id, owner-language
// title, risk string. Resolution still travels through the governed resume
// path (grant phrases as normal turns), never around the broker.
func (g *gatewayRuntime) PendingApproval(sessionKey string) (id, title, risk string, ok bool) {
	var res struct {
		Requests []struct {
			ID         string `json:"id"`
			SessionKey string `json:"session_key"`
			Capability string `json:"capability"`
			Action     string `json:"action"`
			Target     string `json:"target"`
			Risk       string `json:"risk"`
			Card       *struct {
				Title string `json:"title"`
			} `json:"card"`
		} `json:"requests"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := g.getJSON(ctx, "/v1/permissions/requests?status=pending", sessionKey, &res); err != nil {
		return "", "", "", false
	}
	for _, r := range res.Requests {
		if r.SessionKey != "" && r.SessionKey != sessionKey {
			continue
		}
		t := ""
		if r.Card != nil {
			t = strings.TrimSpace(r.Card.Title)
		}
		if t == "" {
			t = strings.TrimSpace(r.Capability + " " + r.Action + " " + r.Target)
		}
		if t == "" {
			t = "Ghost needs your approval"
		}
		return r.ID, t, strings.TrimSpace(r.Risk), true
	}
	return "", "", "", false
}

func (g *gatewayRuntime) CurrentContext(sessionKey string) string {
	g.mu.Lock()
	if time.Since(g.ctxAt) < gatewayCacheTTL && g.ctxCurrent != nil {
		if cur, ok := g.ctxCurrent[sessionKey]; ok {
			defer g.mu.Unlock()
			return cur
		}
	}
	g.mu.Unlock()
	var res struct {
		Current string `json:"current"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := g.getJSON(ctx, "/v1/contexts?session_key="+sessionKey, sessionKey, &res); err != nil || res.Current == "" {
		return "personal"
	}
	g.mu.Lock()
	if g.ctxCurrent == nil {
		g.ctxCurrent = map[string]string{}
	}
	g.ctxCurrent[sessionKey] = res.Current
	g.ctxAt = time.Now()
	g.mu.Unlock()
	return res.Current
}

func (g *gatewayRuntime) ListContexts() []string {
	g.mu.Lock()
	if time.Since(g.ctxAt) < gatewayCacheTTL && g.ctxList != nil {
		defer g.mu.Unlock()
		return append([]string{}, g.ctxList...)
	}
	g.mu.Unlock()
	var res struct {
		Contexts []struct {
			ID string `json:"id"`
		} `json:"contexts"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := g.getJSON(ctx, "/v1/contexts", "", &res); err != nil {
		return []string{"personal"}
	}
	out := make([]string, 0, len(res.Contexts))
	for _, c := range res.Contexts {
		if c.ID != "" {
			out = append(out, c.ID)
		}
	}
	if len(out) == 0 {
		return []string{"personal"}
	}
	g.mu.Lock()
	g.ctxList = append([]string{}, out...)
	g.ctxAt = time.Now()
	g.mu.Unlock()
	return out
}

func (g *gatewayRuntime) SwitchContext(sessionKey, contextID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _, err := g.post(ctx, "/v1/contexts/switch", map[string]string{
		"session_key": sessionKey,
		"context_id":  contextID,
	}, sessionKey)
	if err != nil {
		return err
	}
	// Bust the context cache so the footer and /context read the new
	// value instead of a stale one.
	g.mu.Lock()
	g.ctxAt = time.Time{}
	g.ctxList = nil
	g.mu.Unlock()
	return nil
}

// historyEntry is one backfillable transcript row.
type historyEntry struct {
	Role      string
	Content   string
	Timestamp int64
}

// conversationBackfillCap bounds startup history loads: the whole
// conversation, unless it is truly enormous.
const conversationBackfillCap = 500

// LoadRecentHistory fetches the latest user/assistant rows for a session so
// a freshly started terminal opens on the same conversation the app shows.
func (g *gatewayRuntime) LoadRecentHistory(sessionKey string, limit int) ([]historyEntry, error) {
	return g.LoadConversationHistory(sessionKey, limit)
}

// LoadHistory implements agentRuntime: it backfills one conversation for the
// terminal (used when returning to the shared conversation from a thread).
func (g *gatewayRuntime) LoadHistory(sessionKey string) ([]historyEntry, error) {
	return g.LoadConversationHistory(sessionKey, conversationBackfillCap)
}

// legacySessionAliases are pre-unification names for the one shared
// conversation. The gateway canonicalizes them onto `main`, so requesting
// them on a live gateway returns the main rows — the reason the CLI must
// never treat them as separate conversations (doing so triples the
// transcript).
var legacySessionAliases = map[string]bool{"mobile:default": true, "cli:default": true}

// canonicalConversationKey mirrors the gateway's canonicalSessionID so the
// client can tell when two session names are the same conversation.
func canonicalConversationKey(sessionKey string) string {
	s := strings.TrimSpace(sessionKey)
	if s == "" || legacySessionAliases[s] {
		return MainSessionID
	}
	return s
}

// LoadConversationHistory loads the whole conversation (paged, oldest to
// newest, capped). In the unified model there is exactly one conversation:
// legacy names canonicalize onto it at the gateway, so they are never
// fetched as separate threads (that would duplicate every row).
func (g *gatewayRuntime) LoadConversationHistory(sessionKey string, maxTotal int) ([]historyEntry, error) {
	if maxTotal <= 0 || maxTotal > conversationBackfillCap {
		maxTotal = conversationBackfillCap
	}
	primary, err := g.loadSessionPages(canonicalConversationKey(sessionKey), maxTotal)
	if err != nil {
		return nil, err
	}
	sortByTimestamp(primary)
	if len(primary) > maxTotal {
		primary = primary[len(primary)-maxTotal:]
	}
	return primary, nil
}

// loadSessionPages walks one session newest-page-first and returns rows
// oldest-first, stopping at maxTotal or the last page.
func (g *gatewayRuntime) loadSessionPages(sessionKey string, maxTotal int) ([]historyEntry, error) {
	const pageSize = 100
	var pages [][]historyEntry
	for offset := 0; len(pages)*pageSize < maxTotal; offset += pageSize {
		rows, more, err := g.loadSessionHistory(sessionKey, pageSize, offset)
		if err != nil {
			if offset == 0 {
				return nil, err
			}
			break
		}
		if len(rows) == 0 {
			break
		}
		pages = append(pages, rows)
		if !more {
			break
		}
	}
	var out []historyEntry
	for i := len(pages) - 1; i >= 0; i-- {
		out = append(out, pages[i]...)
	}
	if len(out) > maxTotal {
		out = out[len(out)-maxTotal:]
	}
	return out, nil
}

func sortByTimestamp(rows []historyEntry) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j-1].Timestamp > rows[j].Timestamp; j-- {
			rows[j-1], rows[j] = rows[j], rows[j-1]
		}
	}
}

func (g *gatewayRuntime) loadSessionHistory(sessionKey string, limit, offset int) ([]historyEntry, bool, error) {
	var res struct {
		Messages []struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			Timestamp int64  `json:"timestamp"`
		} `json:"messages"`
		HasMore bool `json:"has_more"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	path := fmt.Sprintf("/v1/history?session=%s&limit=%d&offset=%d", sessionKey, limit, offset)
	if err := g.getJSON(ctx, path, sessionKey, &res); err != nil {
		return nil, false, err
	}
	out := make([]historyEntry, 0, len(res.Messages))
	for _, m := range res.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		out = append(out, historyEntry{Role: m.Role, Content: m.Content, Timestamp: m.Timestamp})
	}
	return out, res.HasMore, nil
}

// ProcessDirectWithChannel runs one turn on the daemon over SSE. Chunks flow
// to onChunk (live rendering, exactly like the embedded loop); server-side
// tool labels arrive complete, so they bypass onToolCall and are forwarded
// as toolProgressMsg directly — recomputing them locally from empty args
// would discard the daemon's wording. Clarify requests are forwarded as
// clarifyRequestMsg so the TUI can answer in-band like the mobile app.
func (g *gatewayRuntime) ProcessDirectWithChannel(ctx context.Context, content, sessionKey, channel, chatID string, media []string, onChunk func(string), onToolCall func(string, string)) (string, error) {
	body := map[string]interface{}{
		"request_id":  fmt.Sprintf("cli-%d", time.Now().UnixNano()),
		"content":     content,
		"session_key": sessionKey,
		"channel":     channel,
		"chat_id":     chatID,
	}
	if tz := localZoneName(); tz != "" {
		body["metadata"] = map[string]string{"timezone": tz}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/v1/chat", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Type", "cli")
	req.Header.Set("X-Ghost-Session", sessionKey)
	resp, err := g.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Non-stream replies: replay/409/error envelopes.
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode == http.StatusConflict {
			return "", fmt.Errorf("that turn is already running; wait for it to finish")
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if msg := gatewayErrorMessage(out); msg != "" {
				return "", fmt.Errorf("%s", msg)
			}
			return "", fmt.Errorf("gateway chat answered %d", resp.StatusCode)
		}
		var replay struct {
			Replay  bool   `json:"replay"`
			Outcome string `json:"outcome"`
		}
		if json.Unmarshal(out, &replay) == nil && replay.Replay {
			return "", fmt.Errorf("turn already recorded as %s", replay.Outcome)
		}
		return "", fmt.Errorf("unexpected gateway reply")
	}

	var text strings.Builder
	outcome := ""
	failedText := ""
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r\n")
		if line == "" || strings.HasPrefix(line, ":") {
			continue // blank separator / keepalive comment
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		// JSON-encoded string chunk: assistant text.
		var chunk string
		if err := json.Unmarshal([]byte(payload), &chunk); err == nil {
			text.WriteString(chunk)
			if onChunk != nil {
				onChunk(chunk)
			}
			continue
		}
		var obj struct {
			Type       string   `json:"type"`
			Tool       string   `json:"tool"`
			Label      string   `json:"label"`
			State      string   `json:"state"`
			Outcome    string   `json:"outcome"`
			QuestionID string   `json:"question_id"`
			Question   string   `json:"question"`
			Choices    []string `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &obj); err != nil {
			continue
		}
		switch obj.Type {
		case "tool_status":
			if agentProgram != nil {
				agentProgram.Send(toolProgressMsg{tool: obj.Tool, label: obj.Label})
			}
		case "clarify_request":
			if agentProgram != nil {
				agentProgram.Send(clarifyRequestMsg{
					questionID: obj.QuestionID,
					question:   obj.Question,
					choices:    obj.Choices,
				})
			}
		case "lifecycle":
			if obj.State == "completed" {
				outcome = obj.Outcome
			}
		case "assistant_message":
			// Text already arrived as chunks; metadata only.
		}
	}
	if err := scanner.Err(); err != nil && !isStreamClosedError(err) {
		return strings.TrimSpace(text.String()), fmt.Errorf("stream interrupted: %w", err)
	}
	final := strings.TrimSpace(text.String())
	if outcome == "failed" {
		if strings.HasPrefix(final, "Error:") {
			failedText = strings.TrimSpace(strings.TrimPrefix(final, "Error:"))
		}
		if failedText == "" {
			failedText = "the turn failed without detail"
		}
		return "", fmt.Errorf("%s", failedText)
	}
	return final, nil
}

func isStreamClosedError(err error) bool {
	if err == nil {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unexpected eof") || strings.Contains(s, "connection reset")
}

// localZoneName reports the device timezone for scheduling ("9 AM" means
// the user's 9 AM). The daemon validates IANA names and falls back to UTC
// when absent or unknown, so only an explicit TZ is sent.
func localZoneName() string {
	return strings.TrimSpace(os.Getenv("TZ"))
}
