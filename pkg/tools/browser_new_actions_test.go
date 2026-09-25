package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordingRun captures the CLI invocation each action makes.
type recordingRun struct {
	action string
	args   []string
	result *ToolResult
	err    error
}

func (r *recordingRun) fn(ctx context.Context, action string, args ...string) *ToolResult {
	r.action = action
	r.args = append([]string{}, args...)
	if r.result != nil {
		return r.result
	}
	return &ToolResult{ForLLM: `{"success":true,"data":{"ok":true}}`, ForUser: "ok"}
}

func newRecordingTool(t *testing.T, action string) (*BrowserTool, *recordingRun) {
	t.Helper()
	bt := NewBrowserTool(t.TempDir(), action)
	rec := &recordingRun{}
	bt.run = rec.fn
	return bt, rec
}

// Every shipped action classifies; observe/act/transact partition is
// what the broker keys on.
func TestBrowserClassifyFullSurface(t *testing.T) {
	want := map[string]string{
		"navigate": "observe", "snapshot": "observe", "wait": "observe",
		"find": "observe", "screenshot": "observe", "scroll": "observe",
		"console": "observe", "network": "observe", "a11y": "observe",
		"click": "act", "type": "act", "press": "act", "fill": "act",
		"select": "act", "check": "act", "hover": "act", "drag": "act",
		"fill_form": "act", "dialog": "act", "upload": "act",
		"download": "act",
		"submit":   "transact",
	}
	for action, w := range want {
		if got := NewBrowserTool("", action).Classify(); got != w {
			t.Fatalf("%s: Classify = %s, want %s", action, got, w)
		}
	}
}

func TestBrowserRefArgs(t *testing.T) {
	cases := []struct {
		action string
		args   map[string]interface{}
		want   []string
	}{
		{"click", map[string]interface{}{"ref": " @e1 "}, []string{"@e1"}},
		{"click", map[string]interface{}{}, nil},
		{"snapshot", map[string]interface{}{"ref": "@e1"}, nil},
		{"drag", map[string]interface{}{"source_ref": "@e2", "target_ref": "@e8"}, []string{"@e2", "@e8"}},
		{"drag", map[string]interface{}{"source_ref": "@e2"}, []string{"@e2"}},
		{"fill_form", map[string]interface{}{"fields": []interface{}{
			map[string]interface{}{"ref": "@e3", "text": "a"},
			map[string]interface{}{"ref": " @e4 ", "text": "b"},
			map[string]interface{}{"text": "no ref"},
		}}, []string{"@e3", "@e4"}},
		{"wait", map[string]interface{}{"selector": "#x"}, nil},
	}
	for _, c := range cases {
		got := browserRefArgs(c.action, c.args)
		if len(got) != len(c.want) {
			t.Fatalf("%s: refArgs = %v, want %v", c.action, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s: refArgs = %v, want %v", c.action, got, c.want)
			}
		}
	}
}

func TestBrowserEpochActionSets(t *testing.T) {
	// Only ref-bearing observations re-open the epoch.
	for _, a := range []string{"navigate", "snapshot", "find"} {
		if !browserActionObserves(a) {
			t.Fatalf("%s must observe", a)
		}
	}
	for _, a := range []string{"wait", "screenshot", "scroll", "console", "network", "a11y"} {
		if browserActionObserves(a) {
			t.Fatalf("%s must NOT re-open the epoch (would invalidate live refs)", a)
		}
		if browserActionMutates(a) {
			t.Fatalf("%s is observe-class; must not close the epoch", a)
		}
	}
	// Every act-class action closes the epoch after success.
	for _, a := range []string{"click", "type", "fill", "press", "submit",
		"select", "check", "hover", "drag", "fill_form", "upload", "download", "dialog"} {
		if !browserActionMutates(a) {
			t.Fatalf("%s must close the epoch", a)
		}
	}
	for _, a := range []string{"find", "screenshot", "console"} {
		if browserActionMutates(a) {
			t.Fatalf("%s must not close the epoch", a)
		}
	}
}

func TestBrowserScrollArgs(t *testing.T) {
	bt, rec := newRecordingTool(t, "scroll")
	if res := bt.Execute(context.Background(), map[string]interface{}{}); res.IsError {
		t.Fatalf("default scroll failed: %s", res.ForLLM)
	}
	if rec.action != "scroll" || rec.args[0] != "down" || rec.args[1] != "300" {
		t.Fatalf("scroll cli = %s %v", rec.action, rec.args)
	}
	if res := bt.Execute(context.Background(), map[string]interface{}{"direction": "UP", "pixels": float64(99999)}); res.IsError {
		t.Fatalf("scroll failed: %s", res.ForLLM)
	}
	if rec.args[0] != "up" || rec.args[1] != "20000" {
		t.Fatalf("scroll cli = %v (normalization/cap broken)", rec.args)
	}
	if res := bt.Execute(context.Background(), map[string]interface{}{"direction": "sideways"}); !res.IsError {
		t.Fatal("invalid direction must fail")
	}
}

func TestBrowserWaitConditions(t *testing.T) {
	bt, rec := newRecordingTool(t, "wait")
	if res := bt.Execute(context.Background(), map[string]interface{}{}); !res.IsError {
		t.Fatal("no condition must fail")
	}
	if res := bt.Execute(context.Background(), map[string]interface{}{"selector": "#a", "text": "b"}); !res.IsError {
		t.Fatal("two conditions must fail (ambiguous wait)")
	}
	if res := bt.Execute(context.Background(), map[string]interface{}{"text": "Hello"}); res.IsError {
		t.Fatalf("text wait failed: %s", res.ForLLM)
	}
	if rec.action != "wait" || rec.args[0] != "--text" || rec.args[1] != "Hello" {
		t.Fatalf("wait cli = %s %v", rec.action, rec.args)
	}
	bt.Execute(context.Background(), map[string]interface{}{"url": "/dash"})
	if rec.args[0] != "--url" || rec.args[1] != "/dash" {
		t.Fatalf("wait url cli = %v", rec.args)
	}
	bt.Execute(context.Background(), map[string]interface{}{"load": "networkidle"})
	if rec.args[0] != "--load" || rec.args[1] != "networkidle" {
		t.Fatalf("wait load cli = %v", rec.args)
	}
	if res := bt.Execute(context.Background(), map[string]interface{}{"load": "whenever"}); !res.IsError {
		t.Fatal("invalid load state must fail")
	}
}

func TestBrowserFindReturnsFreshRefs(t *testing.T) {
	bt, rec := newRecordingTool(t, "find")
	tree := `{"success":true,"data":{"origin":"https://x.test/","refs":{"e1":{"name":"Contact us"}},"snapshot":"- link \"Contact us\" [ref=e1]\n- heading \"About\" [ref=e2]"}}`
	rec.result = &ToolResult{ForLLM: tree, ForUser: tree}
	res := bt.Execute(context.Background(), map[string]interface{}{"text": "contact"})
	if res.IsError {
		t.Fatalf("find failed: %s", res.ForLLM)
	}
	if rec.action != "snapshot" {
		t.Fatalf("find must read a fresh snapshot, ran %s", rec.action)
	}
	if !strings.Contains(res.ForLLM, `ref=e1`) || !strings.Contains(res.ForLLM, "Contact us") {
		t.Fatalf("find must return matching lines with refs: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, "About") {
		t.Fatalf("find must not return non-matching lines: %s", res.ForLLM)
	}
	// No match: honest miss, no error.
	rec.result = &ToolResult{ForLLM: tree, ForUser: tree}
	res = bt.Execute(context.Background(), map[string]interface{}{"text": "zzz-not-there"})
	if res.IsError || !strings.Contains(res.ForLLM, "No element") {
		t.Fatalf("find miss must be honest: %+v", res)
	}
	if res := bt.Execute(context.Background(), map[string]interface{}{}); !res.IsError {
		t.Fatal("find without text must fail")
	}
}

func TestBrowserSelectCheckHoverDrag(t *testing.T) {
	bt, rec := newRecordingTool(t, "select")
	if res := bt.Execute(context.Background(), map[string]interface{}{"ref": "@e1", "value": "KE"}); res.IsError {
		t.Fatalf("select: %s", res.ForLLM)
	}
	if rec.action != "select" || rec.args[0] != "@e1" || rec.args[1] != "KE" {
		t.Fatalf("select cli = %s %v", rec.action, rec.args)
	}

	bt, rec = newRecordingTool(t, "check")
	bt.Execute(context.Background(), map[string]interface{}{"ref": "@e2"})
	if rec.action != "check" {
		t.Fatalf("default check cli = %s", rec.action)
	}
	bt.Execute(context.Background(), map[string]interface{}{"ref": "@e2", "checked": false})
	if rec.action != "uncheck" {
		t.Fatalf("uncheck cli = %s", rec.action)
	}

	bt, rec = newRecordingTool(t, "hover")
	bt.Execute(context.Background(), map[string]interface{}{"ref": "@e3"})
	if rec.action != "hover" || rec.args[0] != "@e3" {
		t.Fatalf("hover cli = %s %v", rec.action, rec.args)
	}

	bt, rec = newRecordingTool(t, "drag")
	if res := bt.Execute(context.Background(), map[string]interface{}{"source_ref": "@e4", "target_ref": "@e9"}); res.IsError {
		t.Fatalf("drag: %s", res.ForLLM)
	}
	if rec.action != "drag" || rec.args[0] != "@e4" || rec.args[1] != "@e9" {
		t.Fatalf("drag cli = %s %v", rec.action, rec.args)
	}
}

func TestBrowserDialogActions(t *testing.T) {
	bt, rec := newRecordingTool(t, "dialog")
	if res := bt.Execute(context.Background(), map[string]interface{}{"action": "status"}); res.IsError {
		t.Fatalf("status: %s", res.ForLLM)
	}
	if rec.action != "dialog" || rec.args[0] != "status" {
		t.Fatalf("dialog status cli = %s %v", rec.action, rec.args)
	}
	bt.Execute(context.Background(), map[string]interface{}{"action": "accept", "text": "yes"})
	if rec.args[0] != "accept" || rec.args[1] != "yes" {
		t.Fatalf("dialog accept cli = %v", rec.args)
	}
	bt.Execute(context.Background(), map[string]interface{}{"action": "dismiss"})
	if rec.args[0] != "dismiss" {
		t.Fatalf("dialog dismiss cli = %v", rec.args)
	}
	if res := bt.Execute(context.Background(), map[string]interface{}{"action": "shrug"}); !res.IsError {
		t.Fatal("unknown dialog action must fail")
	}
}

func TestBrowserFillFormReportsPerField(t *testing.T) {
	bt, rec := newRecordingTool(t, "fill_form")
	filled := []string{}
	rec.result = nil
	bt.run = func(ctx context.Context, action string, args ...string) *ToolResult {
		if action != "fill" {
			t.Fatalf("fill_form must run fill, ran %s", action)
		}
		if args[0] == "@e2" {
			return ErrorResult("element not editable")
		}
		filled = append(filled, args[0])
		return &ToolResult{ForLLM: "ok", ForUser: "ok"}
	}
	res := bt.Execute(context.Background(), map[string]interface{}{"fields": []interface{}{
		map[string]interface{}{"ref": "@e1", "text": "Ian"},
		map[string]interface{}{"ref": "@e2", "text": "blocked"},
	}})
	if res.IsError {
		t.Fatalf("partial fill must report, not error: %s", res.ForLLM)
	}
	if len(filled) != 1 || filled[0] != "@e1" {
		t.Fatalf("filled = %v", filled)
	}
	if !strings.Contains(res.ForLLM, "Filled 1/2") || !strings.Contains(res.ForLLM, "element not editable") {
		t.Fatalf("report must carry both outcomes: %s", res.ForLLM)
	}
	// All failed → error.
	bt.run = func(ctx context.Context, action string, args ...string) *ToolResult {
		return ErrorResult("nope")
	}
	res = bt.Execute(context.Background(), map[string]interface{}{"fields": []interface{}{
		map[string]interface{}{"ref": "@e1", "text": "x"},
	}})
	if !res.IsError {
		t.Fatal("all-failed fill_form must be an error")
	}
	// Bounds.
	if res := bt.Execute(context.Background(), map[string]interface{}{"fields": []interface{}{}}); !res.IsError {
		t.Fatal("empty fields must fail")
	}
	many := make([]interface{}, 11)
	for i := range many {
		many[i] = map[string]interface{}{"ref": "@e1", "text": "x"}
	}
	if res := bt.Execute(context.Background(), map[string]interface{}{"fields": many}); !res.IsError {
		t.Fatal("more than 10 fields must fail")
	}
}

func TestBrowserUploadValidatesPaths(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(good, []byte("hi"), 0600); err != nil {
		t.Fatal(err)
	}
	bt, rec := newRecordingTool(t, "upload")
	if res := bt.Execute(context.Background(), map[string]interface{}{
		"ref": "@e5", "paths": []interface{}{good},
	}); res.IsError {
		t.Fatalf("upload: %s", res.ForLLM)
	}
	if rec.action != "upload" || rec.args[0] != "@e5" || rec.args[1] != good {
		t.Fatalf("upload cli = %s %v", rec.action, rec.args)
	}
	// Missing file refuses before any CLI run.
	calls := 0
	bt.run = func(ctx context.Context, action string, args ...string) *ToolResult {
		calls++
		return &ToolResult{ForLLM: "ok"}
	}
	res := bt.Execute(context.Background(), map[string]interface{}{
		"ref": "@e5", "paths": []interface{}{filepath.Join(dir, "missing.txt")},
	})
	if !res.IsError || !strings.Contains(res.ForLLM, "does not exist") {
		t.Fatalf("missing file must refuse: %+v", res)
	}
	if calls != 0 {
		t.Fatal("refused upload reached the CLI")
	}
	// Directory is not a file.
	if res := bt.Execute(context.Background(), map[string]interface{}{
		"ref": "@e5", "paths": []interface{}{dir},
	}); !res.IsError {
		t.Fatal("directory upload must refuse")
	}
}

func TestBrowserDownloadLandsInManagedDir(t *testing.T) {
	ws := t.TempDir()
	bt := NewBrowserTool(ws, "download")
	var gotPath string
	bt.run = func(ctx context.Context, action string, args ...string) *ToolResult {
		if action != "download" {
			t.Fatalf("download cli action = %s", action)
		}
		gotPath = args[1]
		if !strings.HasPrefix(gotPath, filepath.Join(ws, "state", "browser-downloads")) {
			t.Fatalf("download path %q escapes managed dir", gotPath)
		}
		return &ToolResult{ForLLM: "saved", ForUser: "saved"}
	}
	res := bt.Execute(context.Background(), map[string]interface{}{"ref": "@e7"})
	if res.IsError {
		t.Fatalf("download: %s", res.ForLLM)
	}
	if gotPath == "" || !strings.Contains(res.ForLLM, gotPath) {
		t.Fatalf("result must name the saved path: %s", res.ForLLM)
	}
}

// Outcome-unknown failure semantics: timeouts and transport drops must
// forbid blind retries of mutating actions.
func TestBrowserFailureMessageOutcomeUnknown(t *testing.T) {
	deadlineCtx, cancel := context.WithCancel(context.Background())
	cancel() // ctx.Err() != nil simulates the deadline/kill path
	msg := browserFailureMessage("click", fmt.Errorf("signal: killed"), "", deadlineCtx)
	if !strings.Contains(msg, "[browser.timeout]") || !strings.Contains(msg, "outcome is unknown") {
		t.Fatalf("timeout must be outcome-unknown: %s", msg)
	}
	if !strings.Contains(msg, "Do not repeat a mutating action") {
		t.Fatalf("timeout must forbid blind retries: %s", msg)
	}

	live := context.Background()
	msg = browserFailureMessage("click", fmt.Errorf("exit status 1"),
		"Error: websocket connection to browser closed", live)
	if !strings.Contains(msg, "[browser.disconnected]") || !strings.Contains(msg, "may already have run") {
		t.Fatalf("disconnect must be outcome-unknown: %s", msg)
	}

	msg = browserFailureMessage("click", fmt.Errorf("exit status 1"), "Unknown ref: e3", live)
	if !strings.Contains(msg, "stale element ref") || !strings.Contains(msg, "Re-snapshot") {
		t.Fatalf("unknown ref must steer to re-snapshot: %s", msg)
	}

	msg = browserFailureMessage("fill", fmt.Errorf("exit status 1"), "some ordinary failure", live)
	if !strings.Contains(msg, "Browser action 'fill' failed") {
		t.Fatalf("ordinary failures keep their plain form: %s", msg)
	}
}

func TestBrowserNavigateMergesTree(t *testing.T) {
	bt := NewBrowserTool(t.TempDir(), "navigate")
	nav := `{"success":true,"data":{"title":"Example","url":"https://example.com/"},"error":null}`
	snap := `{"success":true,"data":{"origin":"https://example.com/","refs":{"e1":{"name":"Go"}},"snapshot":"- link \"Go\" [ref=e1]"},"error":null}`
	calls := 0
	bt.run = func(ctx context.Context, action string, args ...string) *ToolResult {
		calls++
		switch action {
		case "navigate":
			if args[0] != "https://example.com/" && args[0] != "https://example.com" {
				t.Fatalf("navigate cli args = %v", args)
			}
			return &ToolResult{ForLLM: nav, ForUser: nav}
		case "snapshot":
			return &ToolResult{ForLLM: snap, ForUser: snap}
		default:
			t.Fatalf("unexpected action %s", action)
		}
		return nil
	}
	res := bt.Execute(context.Background(), map[string]interface{}{"url": "https://example.com"})
	if res.IsError {
		t.Fatalf("navigate: %s", res.ForLLM)
	}
	if calls != 2 {
		t.Fatalf("navigate must fetch identity + tree, calls = %d", calls)
	}
	view, ok := browserPageViewOf(res.ForLLM)
	if !ok || view.URL != "https://example.com/" || view.Title != "Example" {
		t.Fatalf("merged url/title missing: %+v (%s)", view, res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "ref=e1") {
		t.Fatalf("merged tree must carry refs: %s", res.ForLLM)
	}
	// Both wire shapes' fields present for ParseRefs (refs map + tree).
	if !strings.Contains(res.ForLLM, `"refs"`) {
		t.Fatalf("merged payload must keep refs map: %s", res.ForLLM)
	}
}

func TestBrowserPageViewDualShape(t *testing.T) {
	// Legacy flat shape.
	v, ok := browserPageViewOf(`{"url":"https://a.test/","title":"T","text":"body"}`)
	if !ok || v.URL != "https://a.test/" || v.Text != "body" {
		t.Fatalf("legacy shape: %+v", v)
	}
	// Current envelope shape (origin + snapshot).
	v, ok = browserPageViewOf(`{"success":true,"data":{"origin":"https://b.test/","snapshot":"- tree"},"error":null}`)
	if !ok || v.URL != "https://b.test/" || v.Text != "- tree" {
		t.Fatalf("current shape: %+v", v)
	}
	// Non-JSON.
	if _, ok := browserPageViewOf("plain text"); ok {
		t.Fatal("non-JSON must not parse as a page view")
	}
}

func TestSnapshotTreeOf(t *testing.T) {
	tree := snapshotTreeOf(`{"success":true,"data":{"snapshot":"- button [ref=e1]"}}`)
	if tree != "- button [ref=e1]" {
		t.Fatalf("tree = %q", tree)
	}
	if got := snapshotTreeOf("raw"); got != "raw" {
		t.Fatalf("fallback = %q", got)
	}
}

func TestFindTreeLinesBounds(t *testing.T) {
	var tree strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&tree, "- link \"Item %d\" [ref=e%d]\n", i, i+1)
	}
	got := findTreeLines(tree.String(), "item", 20)
	if len(got) != 20 {
		t.Fatalf("matches = %d, want 20 (bounded)", len(got))
	}
	got = findTreeLines("nothing here", "item", 20)
	if len(got) != 0 {
		t.Fatalf("miss = %v", got)
	}
}

func TestAttachPageEvidenceCurrentShape(t *testing.T) {
	res := &ToolResult{
		ForLLM:   `{"success":true,"data":{"origin":"https://news.test/story","title":"Head","snapshot":"- heading \"Head\" [ref=e1]"},"error":null}`,
		Evidence: map[string]interface{}{},
	}
	attachPageEvidence(res)
	if res.Evidence["url"] != "https://news.test/story" {
		t.Fatalf("evidence url = %v", res.Evidence["url"])
	}
	if res.Evidence["domain"] != "news.test" {
		t.Fatalf("evidence domain = %v", res.Evidence["domain"])
	}
	if res.Evidence["title"] != "Head" {
		t.Fatalf("evidence title = %v", res.Evidence["title"])
	}
	if txt, _ := res.Evidence["text"].(string); !strings.Contains(txt, "ref=e1") {
		t.Fatalf("evidence text = %v", res.Evidence["text"])
	}
}

func TestNetworkFilterArgument(t *testing.T) {
	bt, rec := newRecordingTool(t, "network")
	if res := bt.Execute(context.Background(), map[string]interface{}{"filter": "api"}); res.IsError {
		t.Fatalf("network: %s", res.ForLLM)
	}
	if rec.action != "network" || rec.args[0] != "requests" || rec.args[1] != "--filter" || rec.args[2] != "api" {
		t.Fatalf("network cli = %s %v", rec.action, rec.args)
	}
	// Unfiltered output is still projected (headers dropped).
	bt, rec = newRecordingTool(t, "network")
	rec.result = &ToolResult{ForLLM: `{"success":true,"data":{"requests":[{"method":"GET","url":"https://x.test/a","headers":{"Cookie":"secret=1","Authorization":"Bearer sk-123"},"status":200}]}}`, ForUser: "raw"}
	res := bt.Execute(context.Background(), map[string]interface{}{})
	if res.IsError {
		t.Fatalf("network: %s", res.ForLLM)
	}
	var env map[string]interface{}
	if err := json.Unmarshal([]byte(res.ForLLM), &env); err != nil {
		t.Fatalf("projection must stay JSON: %v", err)
	}
	if strings.Contains(res.ForLLM, "Cookie") || strings.Contains(res.ForLLM, "Authorization") || strings.Contains(res.ForLLM, "secret=1") {
		t.Fatalf("headers must never reach the model: %s", res.ForLLM)
	}
	data := env["data"].(map[string]interface{})
	reqs := data["requests"].([]interface{})
	req := reqs[0].(map[string]interface{})
	if req["method"] != "GET" || req["status"] != float64(200) || req["url"] != "https://x.test/a" {
		t.Fatalf("projected request lost signal: %+v", req)
	}
}
