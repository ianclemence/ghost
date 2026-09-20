package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func jsonDecodeBody(r *http.Request, target interface{}) {
	defer r.Body.Close()
	_ = json.NewDecoder(r.Body).Decode(target)
}

// fakeGateway is a scripted Ghost gateway for client tests. Handlers record
// what the CLI sent so tests assert the contract, not just status codes.
type fakeGateway struct {
	mu       sync.Mutex
	steering []map[string]string
	modelSet []string
	modelGet int
	switched [][2]string
	clarify  []map[string]string
	chatSSE  string
	// historyBySession overrides the history payload per ?session=.
	// Absent keys fall back to the default two-row payload.
	historyBySession map[string]string
}

func (f *fakeGateway) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/v1/chat", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, f.chatSSE)
	})
	mux.HandleFunc("/v1/steering", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		jsonDecodeBody(r, &body)
		f.mu.Lock()
		f.steering = append(f.steering, body)
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/v1/permissions/requests", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"requests":[
			{"id":"req-9","session_key":"other:session","capability":"exec","action":"run","risk":"high_impact"},
			{"id":"req-1","session_key":"main","capability":"exec","action":"run","target":"deploy.sh","risk":"consequential","card":{"title":"Run deploy.sh?"}}
		]}`))
	})
	mux.HandleFunc("/v1/model", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body map[string]string
			jsonDecodeBody(r, &body)
			if body["model"] == "bogus" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"kind":"switch_failed","message":"unknown model"}}`))
				return
			}
			f.mu.Lock()
			f.modelSet = append(f.modelSet, body["model"])
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"ok":true,"active":"` + body["model"] + `"}`))
			return
		}
		f.mu.Lock()
		f.modelGet++
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"active":"deepseek-flash","provider":"deepseek","presets":[{"name":"fast"},{"name":"deep"}]}`))
	})
	mux.HandleFunc("/v1/contexts", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"contexts":[{"id":"personal"},{"id":"work"}],"current":"work"}`))
	})
	mux.HandleFunc("/v1/contexts/switch", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		jsonDecodeBody(r, &body)
		if body["context_id"] == "nope" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"kind":"switch_failed","message":"unknown context"}}`))
			return
		}
		f.mu.Lock()
		f.switched = append(f.switched, [2]string{body["session_key"], body["context_id"]})
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true,"context_id":"` + body["context_id"] + `"}`))
	})
	mux.HandleFunc("/v1/history", func(w http.ResponseWriter, r *http.Request) {
		if payload, ok := f.historyBySession[r.URL.Query().Get("session")]; ok {
			_, _ = w.Write([]byte(payload))
			return
		}
		// Default store honors limit/offset newest-first like the real
		// endpoint (DESC page, chronological within the page).
		all := []string{
			`{"role":"user","content":"hello","timestamp":100}`,
			`{"role":"tool","content":"should be skipped","timestamp":101}`,
			`{"role":"assistant","content":"hi there","timestamp":102}`,
			`{"role":"user","content":"   ","timestamp":103}`,
			`{"role":"assistant","content":"bye","timestamp":104}`,
		}
		limit, offset := 50, 0
		_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
		_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
		if limit <= 0 {
			limit = 50
		}
		if offset < 0 {
			offset = 0
		}
		start := len(all) - offset - limit
		if start < 0 {
			start = 0
		}
		end := len(all) - offset
		if end < 0 {
			end = 0
		}
		if end > len(all) {
			end = len(all)
		}
		page := all[start:end]
		fmt.Fprintf(w, `{"messages":[%s],"total":%d,"has_more":%v}`, strings.Join(page, ","), len(page), start > 0)
	})
	mux.HandleFunc("/v1/clarify/respond", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		jsonDecodeBody(r, &body)
		f.mu.Lock()
		f.clarify = append(f.clarify, body)
		f.mu.Unlock()
		if body["question_id"] == "gone" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"kind":"not_found","message":"expired"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	return mux
}

func newTestGateway(t *testing.T, chatSSE string) (*gatewayRuntime, *fakeGateway) {
	t.Helper()
	fg := &fakeGateway{chatSSE: chatSSE}
	srv := httptest.NewServer(fg.handler())
	t.Cleanup(srv.Close)
	return &gatewayRuntime{baseURL: srv.URL, http: srv.Client()}, fg
}

func TestGatewayReachable(t *testing.T) {
	fg := &fakeGateway{}
	srv := httptest.NewServer(fg.handler())
	defer srv.Close()
	if !gatewayReachable(srv.URL) {
		t.Errorf("live test server must be reachable")
	}
	if gatewayReachable("http://127.0.0.1:1") {
		t.Errorf("closed port must be unreachable")
	}
}

func TestGatewayChatStream(t *testing.T) {
	sse := "data: {\"type\":\"lifecycle\",\"state\":\"agent_processing\"}\n\n" +
		"data: \"Hello\"\n\n" +
		"data: {\"type\":\"tool_status\",\"tool\":\"exec\",\"label\":\"Running: ls\"}\n\n" +
		"data: \" world\"\n\n" +
		"data: {\"type\":\"lifecycle\",\"state\":\"completed\",\"outcome\":\"success\"}\n\n" +
		"data: [DONE]\n\n"
	gw, _ := newTestGateway(t, sse)
	var chunks []string
	text, err := gw.ProcessDirectWithChannel(context.Background(), "hi", "main", "cli", "direct", nil,
		func(s string) { chunks = append(chunks, s) }, nil)
	if err != nil {
		t.Fatalf("stream must succeed: %v", err)
	}
	if text != "Hello world" {
		t.Errorf("chunks must accumulate verbatim, got %q", text)
	}
	if len(chunks) != 2 || chunks[0] != "Hello" || chunks[1] != " world" {
		t.Errorf("onChunk must fire per chunk, got %v", chunks)
	}
}

func TestGatewayChatFailed(t *testing.T) {
	sse := "data: \"Error: thinking engine offline\"\n\n" +
		"data: {\"type\":\"lifecycle\",\"state\":\"completed\",\"outcome\":\"failed\"}\n\n" +
		"data: [DONE]\n\n"
	gw, _ := newTestGateway(t, sse)
	_, err := gw.ProcessDirectWithChannel(context.Background(), "hi", "main", "cli", "direct", nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "thinking engine offline") {
		t.Fatalf("failed turns must surface the daemon error, got %v", err)
	}
}

func TestGatewaySteeringAndAbort(t *testing.T) {
	gw, fg := newTestGateway(t, "")
	gw.InjectSteering("main", "change of plan")
	gw.AbortTurn("main")
	fg.mu.Lock()
	defer fg.mu.Unlock()
	if len(fg.steering) != 2 {
		t.Fatalf("expected 2 steering posts, got %v", fg.steering)
	}
	if fg.steering[0]["action"] != "redirect" || fg.steering[0]["content"] != "change of plan" {
		t.Errorf("redirect payload wrong: %v", fg.steering[0])
	}
	if fg.steering[1]["action"] != "abort" || fg.steering[1]["session_key"] != "main" {
		t.Errorf("abort payload wrong: %v", fg.steering[1])
	}
}

func TestGatewayApprovalMapping(t *testing.T) {
	gw, _ := newTestGateway(t, "")
	id, title, risk, ok := gw.PendingApproval("main")
	if !ok {
		t.Fatalf("pending request for the session must be reported")
	}
	if id != "req-1" || title != "Run deploy.sh?" || risk != "consequential" {
		t.Errorf("approval mapping wrong: %q %q %q", id, title, risk)
	}
	if _, _, _, ok := gw.PendingApproval("cli:something-else"); ok {
		t.Errorf("other sessions' requests must not match")
	}
}

func TestGatewayModel(t *testing.T) {
	gw, fg := newTestGateway(t, "")
	if got := gw.GetCurrentModel(); got != "deepseek-flash" {
		t.Errorf("active model wrong: %q", got)
	}
	if presets := gw.ModelPresets(); len(presets) != 2 || presets[0] != "fast" {
		t.Errorf("presets wrong: %v", presets)
	}
	if err := gw.SetModel("deep"); err != nil {
		t.Errorf("switch must succeed: %v", err)
	}
	if err := gw.SetModel("bogus"); err == nil || !strings.Contains(err.Error(), "unknown model") {
		t.Errorf("failed switch must surface the daemon message, got %v", err)
	}
	fg.mu.Lock()
	defer fg.mu.Unlock()
	if len(fg.modelSet) != 1 || fg.modelSet[0] != "deep" {
		t.Errorf("switch payload wrong: %v", fg.modelSet)
	}
}

// Footer-speed reads must not hit the network every frame: the second
// GetCurrentModel inside the TTL serves the cache.
func TestGatewayModelCache(t *testing.T) {
	gw, fg := newTestGateway(t, "")
	if got := gw.GetCurrentModel(); got != "deepseek-flash" {
		t.Fatalf("active model wrong: %q", got)
	}
	if got := gw.GetCurrentModel(); got != "deepseek-flash" {
		t.Fatalf("cached model wrong: %q", got)
	}
	fg.mu.Lock()
	defer fg.mu.Unlock()
	if fg.modelGet != 1 {
		t.Errorf("expected 1 model GET, got %d", fg.modelGet)
	}
}

func TestGatewayContexts(t *testing.T) {
	gw, fg := newTestGateway(t, "")
	if got := gw.CurrentContext("main"); got != "work" {
		t.Errorf("current context wrong: %q", got)
	}
	if ids := gw.ListContexts(); len(ids) != 2 || ids[0] != "personal" {
		t.Errorf("context list wrong: %v", ids)
	}
	if err := gw.SwitchContext("main", "personal"); err != nil {
		t.Errorf("switch must succeed: %v", err)
	}
	if err := gw.SwitchContext("main", "nope"); err == nil {
		t.Errorf("unknown context must fail")
	}
	fg.mu.Lock()
	defer fg.mu.Unlock()
	if len(fg.switched) != 1 || fg.switched[0] != [2]string{"main", "personal"} {
		t.Errorf("switch payload wrong: %v", fg.switched)
	}
}

func TestGatewayHistory(t *testing.T) {
	gw, _ := newTestGateway(t, "")
	hist, more, err := gw.loadSessionHistory("main", 20, 0)
	if err != nil {
		t.Fatalf("history must load: %v", err)
	}
	if more {
		t.Errorf("full store in one page must not report more")
	}
	if len(hist) != 3 || hist[0].Role != "user" || hist[1].Role != "assistant" || hist[2].Role != "assistant" {
		t.Fatalf("tool rows and blanks must be skipped, got %+v", hist)
	}
	if hist[0].Content != "hello" || hist[2].Content != "bye" {
		t.Errorf("history content wrong: %+v", hist)
	}
}

func TestGatewayHistoryPages(t *testing.T) {
	gw, _ := newTestGateway(t, "")
	// Newest-first paging: offset 0 takes the tail, offset 2 the head.
	p0, more0, err := gw.loadSessionHistory("main", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !more0 {
		t.Errorf("first page must report more")
	}
	if len(p0) != 1 || p0[0].Content != "bye" {
		t.Fatalf("page 0 wrong (blank user row skipped): %+v", p0)
	}
	p1, _, err := gw.loadSessionHistory("main", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	// Note: like the real endpoint, has_more counts unfiltered rows, so a
	// page of only-filtered rows can still report more. The walker keeps
	// fetching until an empty page or has_more=false.
	if len(p1) != 1 || p1[0].Content != "hi there" {
		t.Fatalf("page 1 wrong: %+v", p1)
	}
	// Whole-conversation load assembles oldest-first and caps the tail.
	full, err := gw.LoadConversationHistory("other:session", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 2 || full[0].Content != "hi there" || full[1].Content != "bye" {
		t.Fatalf("capped load must keep the newest chronological tail, got %+v", full)
	}
}

// Pre-unification rows (mobile:/cli:default) merge chronologically into
// the backfill so past chats survive the rename on devices whose gateway
// never ran the v6 migration (embedded-only use).
func TestGatewayHistoryMergesLegacy(t *testing.T) {
	gw, fg := newTestGateway(t, "")
	fg.historyBySession = map[string]string{
		"main":            `{"messages":[{"role":"user","content":"new","timestamp":300}],"total":1}`,
		"mobile:default":  `{"messages":[{"role":"user","content":"old app","timestamp":100}],"total":1}`,
		"cli:default":     `{"messages":[{"role":"assistant","content":"old cli","timestamp":200}],"total":1}`,
		"unrelated:voice": `{"messages":[{"role":"user","content":"nope","timestamp":400}],"total":1}`,
	}
	hist, err := gw.LoadConversationHistory("main", 20)
	if err != nil {
		t.Fatalf("merge must load: %v", err)
	}
	if len(hist) != 3 {
		t.Fatalf("expected 3 merged rows, got %+v", hist)
	}
	if hist[0].Content != "old app" || hist[1].Content != "old cli" || hist[2].Content != "new" {
		t.Errorf("rows must merge chronologically, got %+v", hist)
	}
}

// Legacy session names canonicalize onto main at the gateway edge so old
// apps, scripts, and relay clients keep working after the rename.
func TestResolveSessionCanonical(t *testing.T) {
	for in, want := range map[string]string{
		"":                  "main",
		"mobile:default":    "main",
		"cli:default":       "main",
		"main":              "main",
		"voice:note":        "voice:note",
		"cli:custom-thread": "cli:custom-thread",
	} {
		req := httptest.NewRequest(http.MethodGet, "/v1/history", nil)
		if in != "" {
			req.Header.Set("X-Ghost-Session", in)
		}
		if got := resolveSession(req); got != want {
			t.Errorf("resolveSession(%q) = %q, want %q", in, got, want)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/history?session=cli:default", nil)
	if got := resolveSession(req); got != "main" {
		t.Errorf("query session must canonicalize, got %q", got)
	}
}

func TestGatewayClarifyRespond(t *testing.T) {
	gw, fg := newTestGateway(t, "")
	if !gw.RespondClarify("q-1", "blue") {
		t.Errorf("answer must land")
	}
	if gw.RespondClarify("gone", "blue") {
		t.Errorf("expired question must report false")
	}
	fg.mu.Lock()
	defer fg.mu.Unlock()
	if len(fg.clarify) != 2 || fg.clarify[0]["response"] != "blue" {
		t.Errorf("clarify payload wrong: %v", fg.clarify)
	}
}

func TestGatewayUnreachableSurfaces(t *testing.T) {
	gw := &gatewayRuntime{baseURL: "http://127.0.0.1:1", http: http.DefaultClient}
	if got := gw.GetCurrentModel(); got != "unknown" {
		t.Errorf("model must degrade, got %q", got)
	}
	if ids := gw.ListContexts(); len(ids) != 1 || ids[0] != "personal" {
		t.Errorf("contexts must degrade, got %v", ids)
	}
	if _, _, _, ok := gw.PendingApproval("main"); ok {
		t.Errorf("approvals must degrade to none")
	}
	if _, err := gw.LoadRecentHistory("main", 20); err == nil {
		t.Errorf("history must fail cleanly")
	}
	if gw.RespondClarify("q", "a") {
		t.Errorf("clarify must report false")
	}
	if err := gw.SwitchContext("main", "work"); err == nil {
		t.Errorf("switch must fail cleanly")
	}
	if err := gw.SetModel("x"); err == nil {
		t.Errorf("set must fail cleanly")
	}
}
