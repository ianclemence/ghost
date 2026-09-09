package tools

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/browser"
)

// newEnforcedTool builds a REAL BrowserTool on the enforced path: gate
// binding in ctx, real session ledger, stubbed CLI runner.
func newEnforcedTool(t *testing.T, action string) (*BrowserTool, *browser.SessionStore) {
	t.Helper()
	sessions, err := browser.NewSessionStore(testBrowserDB(t), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bt := NewBrowserTool("", action)
	bt.run = func(ctx context.Context, action string, args ...string) *ToolResult {
		return &ToolResult{ForLLM: "page [sk-0123456789abcdef0123456789abcdef] now", ForUser: "page now"}
	}
	return bt, sessions
}

func testBrowserDB(t *testing.T) *sql.DB {
	t.Helper()
	return openBrowserTestDB(t)
}

func enforcedCtx(call BrowserCall) context.Context {
	return WithBrowserCall(context.Background(), call)
}

// G. Pinned-session mismatch fails closed along every dimension.
func TestEnforcedSessionMismatchDenied(t *testing.T) {
	sessions, err := browser.NewSessionStore(testBrowserDB(t), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.GetOrCreate("ian", "personal", "task-a", "default", 0)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		owner string
		ctx   string
		task  string
	}{
		{"owner", "mallory", "personal", "task-a"},
		{"context", "ian", "work", "task-a"},
		{"task", "ian", "personal", "task-b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bt := NewBrowserTool("", "click")
			bt.run = fakeRun("ok")
			call := BrowserCall{
				Owner: c.owner, ContextID: c.ctx, TaskID: c.task,
				SessionID: sess.ID, Sessions: sessions, Op: "click", Permission: "once",
			}
			res := bt.Execute(enforcedCtx(call), map[string]interface{}{"ref": "@e1"})
			if !res.IsError || !strings.Contains(res.ForLLM, "different owner, context, or task") {
				t.Fatalf("session mismatch must deny: %+v", res)
			}
		})
	}
}

// H. Expired pinned session fails closed on resume.
func TestEnforcedExpiredSessionDenied(t *testing.T) {
	bt, sessions := newEnforcedTool(t, "click")
	sess, err := sessions.GetOrCreate("ian", "personal", "task-a", "default", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.Close(sess.ID); err != nil {
		t.Fatal(err)
	}
	row, err := sessions.Get(sess.ID)
	if err != nil || row == nil {
		t.Fatalf("get: %v", err)
	}
	if time.Now().UTC().Before(row.ExpiresAt) {
		t.Fatal("Close must expire the session")
	}
	call := BrowserCall{
		Owner: "ian", ContextID: "personal", TaskID: "task-a",
		SessionID: sess.ID, Sessions: sessions, Op: "click", Permission: "once",
	}
	res := bt.Execute(enforcedCtx(call), map[string]interface{}{"ref": "@e1"})
	if !res.IsError || !strings.Contains(res.ForLLM, "expired") {
		t.Fatalf("expired session must deny: %+v", res)
	}
}

// Act-class work without a broker authorization never reaches the executor.
func TestEnforcedActWithoutPermissionDenied(t *testing.T) {
	bt, sessions := newEnforcedTool(t, "click")
	calls := 0
	bt.run = func(ctx context.Context, action string, args ...string) *ToolResult {
		calls++
		return &ToolResult{ForLLM: "clicked", ForUser: "clicked"}
	}
	call := BrowserCall{Owner: "ian", ContextID: "personal", TaskID: "task-a", Sessions: sessions, Op: "click"}
	res := bt.Execute(enforcedCtx(call), map[string]interface{}{"ref": "@e1"})
	if !res.IsError || !strings.Contains(res.ForLLM, "broker authorization") {
		t.Fatalf("act without permission must deny: %+v", res)
	}
	if calls != 0 {
		t.Fatal("denied act reached the executor")
	}
}

// Operation binding mismatch fails closed: the gate's op is authoritative,
// forged tool selection cannot upgrade an approve-observe into a click.
func TestEnforcedOpBindingMismatchDenied(t *testing.T) {
	bt, sessions := newEnforcedTool(t, "click")
	calls := 0
	bt.run = func(ctx context.Context, action string, args ...string) *ToolResult {
		calls++
		return &ToolResult{ForLLM: "clicked", ForUser: "clicked"}
	}
	// Gate authorized "observe" (read-only), model calls the click tool.
	call := BrowserCall{Owner: "ian", ContextID: "personal", TaskID: "task-a", Sessions: sessions, Op: "snapshot", Permission: "allow"}
	res := bt.Execute(enforcedCtx(call), map[string]interface{}{"ref": "@e1"})
	if !res.IsError || !strings.Contains(res.ForLLM, "operation binding mismatch") {
		t.Fatalf("op mismatch must deny: %+v", res)
	}
	if calls != 0 {
		t.Fatal("mismatched op reached the executor")
	}
}

// A. Allowed observe executes, labels output untrusted, and records
// evidence matching the actual session used.
func TestEnforcedObserveExecutesAndEvidences(t *testing.T) {
	bt, sessions := newEnforcedTool(t, "snapshot")
	call := BrowserCall{Owner: "ian", ContextID: "personal", TaskID: "task-a", Sessions: sessions, Op: "snapshot", Permission: "allow"}
	res := bt.Execute(enforcedCtx(call), map[string]interface{}{})
	if res.IsError {
		t.Fatalf("observe failed: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "UNTRUSTED WEB CONTENT") {
		t.Fatal("page output must be labeled untrusted")
	}
	if strings.Contains(res.ForLLM, "sk-0123456789") {
		t.Fatal("secrets must be redacted from model-bound output")
	}
	if res.ForUser != "page now" {
		t.Fatal("user-visible text must stay unaltered")
	}
	if res.Evidence == nil {
		t.Fatal("governed execution must record evidence")
	}
	// L. The recorded session is the actual row used.
	evID, _ := res.Evidence["session"].(string)
	row, err := sessions.Get(evID)
	if err != nil || row == nil {
		t.Fatalf("evidence session %q must exist: %v", evID, err)
	}
	if row.Owner != "ian" || row.ContextID != "personal" || row.TaskID != "task-a" {
		t.Fatalf("evidence session must match binding: %+v", row)
	}
	if ev := res.Evidence["op"]; ev != "browser.snapshot" {
		t.Fatalf("evidence op = %v", ev)
	}
	if res.Evidence["permission"] != "allow" || res.Evidence["owner"] != "ian" {
		t.Fatalf("evidence binding missing: %+v", res.Evidence)
	}
}

// Allowed act executes exactly once and records evidence.
func TestEnforcedActExecutesWithPermission(t *testing.T) {
	bt, sessions := newEnforcedTool(t, "click")
	calls := 0
	bt.run = func(ctx context.Context, action string, args ...string) *ToolResult {
		calls++
		return &ToolResult{ForLLM: "clicked @e1", ForUser: "clicked"}
	}
	call := BrowserCall{Owner: "ian", ContextID: "personal", TaskID: "task-a", Sessions: sessions, Op: "click", Permission: "grant:once:x"}
	res := bt.Execute(enforcedCtx(call), map[string]interface{}{"ref": "@e1"})
	if res.IsError {
		t.Fatalf("click failed: %s", res.ForLLM)
	}
	if calls != 1 {
		t.Fatalf("executor calls = %d, want 1", calls)
	}
	if res.Evidence["op"] != "browser.click" || res.Evidence["outcome"] != "ok" {
		t.Fatalf("evidence wrong: %+v", res.Evidence)
	}
}

// No owner / no session ledger / no work item each fail closed.
func TestEnforcedMissingBindingDenied(t *testing.T) {
	sessions, _ := browser.NewSessionStore(testBrowserDB(t), t.TempDir())
	bt := NewBrowserTool("", "snapshot")
	bt.run = fakeRun("x")
	cases := []struct {
		name string
		call BrowserCall
		ctx  context.Context
	}{
		{"no ledger", BrowserCall{Owner: "ian", ContextID: "personal", TaskID: "task-a", Op: "snapshot"}, context.Background()},
		{"no owner", BrowserCall{Sessions: sessions, ContextID: "personal", TaskID: "task-a", Op: "snapshot"}, context.Background()},
		{"no work item", BrowserCall{Owner: "ian", Sessions: sessions, ContextID: "personal", Op: "snapshot"}, context.Background()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := bt.Execute(WithBrowserCall(c.ctx, c.call), map[string]interface{}{})
			if !res.IsError {
				t.Fatalf("must deny: %+v", res)
			}
		})
	}
}

// Legacy path is preserved: no gate binding and nil policy runs bare,
// exactly as before the enforced path existed.
func TestEnforcedPreservesLegacyBarePath(t *testing.T) {
	bt := NewBrowserTool("", "snapshot")
	raw := "raw [sk-0123456789abcdef0123456789abcdef]"
	bt.run = fakeRun(raw)
	res := bt.Execute(context.Background(), map[string]interface{}{})
	if res.IsError || res.ForLLM != raw {
		t.Fatalf("legacy path altered: %+v", res)
	}
	if res.Evidence != nil {
		t.Fatal("legacy path must not fabricate evidence")
	}
}
