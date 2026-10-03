package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/browser"
)

// A page that re-renders between one step and the next is the normal
// condition on a real site: a modal opens, a list reorders, a component
// swaps its tree. Ghost used to answer that with two wasted round trips —
// refuse, then make the model ask for a snapshot. These tests pin the
// recovery: the refusal comes back with the page as it is now, and the
// very next act uses a live ref from it.

// staleScenario wires a real enforced click whose CLI is scriptable:
// every action is recorded, and the caller decides what each returns.
func staleScenario(t *testing.T, respond func(action string, n int) *ToolResult) (*BrowserTool, *browser.SessionStore, func(action string) int) {
	t.Helper()
	bt, sessions := newEnforcedTool(t, "click")
	seen := map[string]int{}
	bt.run = func(ctx context.Context, action string, args ...string) *ToolResult {
		seen[action]++
		return respond(action, seen[action])
	}
	return bt, sessions, func(action string) int { return seen[action] }
}

const freshPageJSON = `{"url":"https://shop.test/cart","text":"@e1 [button] Pay now @e2 [link] Continue browsing"}`

// The model snapshot, the page re-rendered underneath it, and the ref it
// named no longer resolves. The refusal must hand back the page so the
// model can pick a live ref straight away — and must not, under any
// reading, click the ref it just refused.
func TestStaleRefHandsBackTheLivePage(t *testing.T) {
	bt, sessions, count := staleScenario(t, func(action string, n int) *ToolResult {
		if action == "click" {
			return &ToolResult{ForLLM: "clicked", ForUser: "clicked"}
		}
		return &ToolResult{ForLLM: freshPageJSON, ForUser: "cart"}
	})
	call := BrowserCall{Owner: "ian", ContextID: "personal", TaskID: "task-a",
		Sessions: sessions, Op: "click", Permission: "grant:once:x"}

	// Open the epoch with a page that has @e1 on something else.
	snapper := NewBrowserTool("", "snapshot")
	snapper.run = func(ctx context.Context, action string, args ...string) *ToolResult {
		return &ToolResult{ForLLM: `{"url":"https://shop.test/","text":"@e1 [link] Sign in"}`, ForUser: "home"}
	}
	if sres := snapper.Execute(enforcedCtx(BrowserCall{
		Owner: "ian", ContextID: "personal", TaskID: "task-a",
		Sessions: sessions, Op: "snapshot", Permission: "allow",
	}), map[string]interface{}{}); sres.IsError {
		t.Fatalf("snapshot failed: %s", sres.ForLLM)
	}
	// One mutation closes the epoch; the next act is now stale.
	if res := bt.Execute(enforcedCtx(call), map[string]interface{}{"ref": "@e1"}); res.IsError {
		t.Fatalf("first click should land: %s", res.ForLLM)
	}

	res := bt.Execute(enforcedCtx(call), map[string]interface{}{"ref": "@e1"})
	if !res.IsError {
		t.Fatalf("a ref from a closed epoch must be refused: %+v", res)
	}
	if count("click") != 1 {
		t.Fatalf("the refused ref must never be executed (clicks=%d)", count("click"))
	}
	if count("snapshot") == 0 {
		t.Fatal("Ghost must look at the page itself rather than send the model back for it")
	}
	if !strings.Contains(res.ForLLM, "shop.test/cart") || !strings.Contains(res.ForLLM, "Pay now") {
		t.Fatalf("refusal does not carry the live page: %s", res.ForLLM)
	}
	// Page text is web content: it arrives marked, like any observation.
	if !strings.Contains(res.ForLLM, "<<<UNTRUSTED WEB CONTENT>>>") {
		t.Fatalf("recovered page entered model context unmarked: %s", res.ForLLM)
	}
	// The epoch is open again, so the next act needs no extra call.
	if res := bt.Execute(enforcedCtx(call), map[string]interface{}{"ref": "@e2"}); res.IsError {
		t.Fatalf("a ref from the recovered page must be live: %s", res.ForLLM)
	}
	if count("click") != 2 {
		t.Fatalf("clicks = %d, want 2 (the recovered one plus the refused one)", count("click"))
	}
}

// The ledger can approve a ref the page then disagrees with: the CLI
// refuses an unknown ref before dispatching. Same recovery.
func TestCLIUnknownRefIsRecoveredTheSameWay(t *testing.T) {
	bt, sessions, count := staleScenario(t, func(action string, n int) *ToolResult {
		if action == "click" {
			return ErrorResult(fmt.Sprintf("Browser 'click' refused a stale element ref: unknown ref %q", "@e7"))
		}
		return &ToolResult{ForLLM: freshPageJSON, ForUser: "cart"}
	})
	snapper := NewBrowserTool("", "snapshot")
	snapper.run = func(ctx context.Context, action string, args ...string) *ToolResult {
		return &ToolResult{ForLLM: `{"url":"https://shop.test/","text":"@e1 [link] Sign in"}`, ForUser: "home"}
	}
	if sres := snapper.Execute(enforcedCtx(BrowserCall{
		Owner: "ian", ContextID: "personal", TaskID: "task-a",
		Sessions: sessions, Op: "snapshot", Permission: "allow",
	}), map[string]interface{}{}); sres.IsError {
		t.Fatalf("snapshot failed: %s", sres.ForLLM)
	}

	res := bt.Execute(enforcedCtx(BrowserCall{
		Owner: "ian", ContextID: "personal", TaskID: "task-a",
		Sessions: sessions, Op: "click", Permission: "grant:once:x",
	}), map[string]interface{}{"ref": "@e1"})
	if !res.IsError {
		t.Fatal("the refused ref must stay an error: nothing ran")
	}
	if !strings.Contains(res.ForLLM, "shop.test/cart") {
		t.Fatalf("CLI refusal was not recovered: %s", res.ForLLM)
	}
	if count("snapshot") == 0 {
		t.Fatal("expected a look at the page after the CLI refused the ref")
	}
	// The action still records as an error: Ghost did not do what was asked.
	if res.Evidence["outcome"] != "error" {
		t.Fatalf("a recovered refusal must still record outcome=error: %+v", res.Evidence)
	}
}

// When the page cannot be read either — the browser is gone — there is
// nothing to heal with, so the original refusal stands word for word
// rather than being replaced by a second, vaguer failure.
func TestStaleRefHealFallsBackToThePlainRefusal(t *testing.T) {
	bt, sessions, count := staleScenario(t, func(action string, n int) *ToolResult {
		return ErrorResult("browser is not running")
	})
	res := bt.Execute(enforcedCtx(BrowserCall{
		Owner: "ian", ContextID: "personal", TaskID: "task-a",
		Sessions: sessions, Op: "click", Permission: "grant:once:x",
	}), map[string]interface{}{"ref": "@e1"})
	if !res.IsError {
		t.Fatal("unobserved ref must be refused")
	}
	if !strings.Contains(res.ForLLM, "stale element ref") {
		t.Fatalf("want the original refusal, got: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, "browser is not running") {
		t.Fatalf("a failed look must not replace the refusal: %s", res.ForLLM)
	}
	if count("click") != 0 {
		t.Fatal("the refused ref must never be executed")
	}
}

// Recovery never widens authority: it reads the page, which any turn may
// do with browser_snapshot, and never acts on it on the model's behalf.
func TestStaleRefHealOnlyObserves(t *testing.T) {
	bt, sessions, count := staleScenario(t, func(action string, n int) *ToolResult {
		return &ToolResult{ForLLM: freshPageJSON, ForUser: "cart"}
	})
	call := BrowserCall{Owner: "ian", ContextID: "personal", TaskID: "task-a",
		Sessions: sessions, Op: "click", Permission: "grant:once:x"}
	if res := bt.Execute(enforcedCtx(call), map[string]interface{}{"ref": "@e1"}); !res.IsError {
		t.Fatal("unobserved ref must be refused")
	}
	for action, n := range map[string]int{
		"click": count("click"), "type": count("type"), "fill": count("fill"),
		"submit": count("submit"), "press": count("press"),
	} {
		if n != 0 {
			t.Fatalf("recovery performed a %s — it may only look", action)
		}
	}
}
