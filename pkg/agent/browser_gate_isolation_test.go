package agent

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/contexts"
	"github.com/ianclemence/ghost/pkg/permissions"
)

// personalContextID resolves the device's default personal context.
func personalContextID(t *testing.T, cs *contexts.Store) string {
	t.Helper()
	for _, c := range cs.List() {
		if c.Kind == contexts.KindPersonal {
			return c.ID
		}
	}
	t.Fatal("no personal context")
	return ""
}

// Isolation across everything: a browser flow minted under Work and one
// under Personal share neither sessions nor authority. The same owner, the
// same tool, two contexts — full stack (gate binding → broker → ledger).
func TestBrowserIsolationAcrossContexts(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	personal := personalContextID(t, h.contexts)
	if err := h.contexts.SetSessionContext("sess-work", h.workCtx); err != nil {
		t.Fatal(err)
	}
	if err := h.contexts.SetSessionContext("sess-pers", personal); err != nil {
		t.Fatal(err)
	}

	// Navigate (read-only) succeeds in both contexts but mints two
	// distinct sessions — the Work cookie jar can never be the Personal one.
	dw := al.authorizeBrowserCall("req-w-1", "sess-work", "browser_navigate", map[string]interface{}{"url": "https://work.example.com"})
	if dw.decision != "allow" {
		t.Fatalf("work navigate must allow: %s", dw.decision)
	}
	if dw.call.ContextID != h.workCtx || dw.call.Owner != h.ghostID || dw.call.TaskID != "sess-work" {
		t.Fatalf("work binding wrong: %+v", dw.call)
	}
	dp := al.authorizeBrowserCall("req-p-1", "sess-pers", "browser_navigate", map[string]interface{}{"url": "https://personal.example.com"})
	if dp.decision != "allow" {
		t.Fatalf("personal navigate must allow: %s", dp.decision)
	}
	if dp.call.ContextID == h.workCtx || dp.call.ContextID != personal {
		t.Fatalf("personal binding wrong: %+v", dp.call)
	}
	// What the real tool would mint: one ledger row per owner+context+task.
	// Same owner, two contexts → two distinct, non-interchangeable rows.
	sessions := dw.call.Sessions
	workRow, err := sessions.GetOrCreate(h.ghostID, h.workCtx, "sess-work", "default", 0)
	if err != nil {
		t.Fatal(err)
	}
	personalRow, err := sessions.GetOrCreate(h.ghostID, personal, "sess-pers", "default", 0)
	if err != nil {
		t.Fatal(err)
	}
	if workRow.ID == personalRow.ID {
		t.Fatal("Work and Personal must never share a browser session row")
	}
}

// An adversarial caller holding a Work-minted session ID cannot drive it
// from Personal by forging its context in arguments or in a resume: the
// pinned row's context must equal the live session context.
func TestForgedContextCannotUseWorkSession(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	personal := personalContextID(t, h.contexts)
	if err := h.contexts.SetSessionContext("sess-attack", personal); err != nil {
		t.Fatal(err)
	}
	// Mint a Work session for the personal-attacker session? No: sessions
	// are keyed by owner/context/task. Instead steal a session minted
	// under Work for a different (victim) task and try to use it from the
	// personal session by pinning it and forging the context argument.
	if err := h.contexts.SetSessionContext("sess-victim", h.workCtx); err != nil {
		t.Fatal(err)
	}
	dv := al.authorizeBrowserCall("req-v-1", "sess-victim", "browser_navigate", map[string]interface{}{"url": "https://victim.example.com"})
	// The victim's real browser session lives in the ledger under Work.
	victimRow, err := dv.call.Sessions.GetOrCreate(h.ghostID, h.workCtx, "sess-victim", "default", 0)
	if err != nil {
		t.Fatal(err)
	}
	victimSess := victimRow.ID

	// Attacker session is personal; it forges "context: work" and the
	// victim session ID in arguments. The gate ignores forged context
	// args (J) — the binding derives the attacker's own personal context
	// and mints/uses a personal session, never the victim's.
	datt := al.authorizeBrowserCall("req-att-1", "sess-attack", "browser_navigate", map[string]interface{}{
		"url": "https://evil.example.com", "context": "work", "browser_session": victimSess,
	})
	if datt.decision != "allow" {
		t.Fatalf("attacker observe must allow under own context: %s", datt.decision)
	}
	if datt.call.ContextID == h.workCtx {
		t.Fatalf("forged context must not bind the work context: %+v", datt.call)
	}
	if datt.call.SessionID != "" {
		t.Fatal("gate must not honor a forged session id on fresh calls")
	}
	// The equivalent refusal at the tool layer — a pinned victim session
	// whose stored context is Work cannot run under a personal binding —
	// is proven by the real BrowserTool in pkg/tools
	// (TestEnforcedSessionMismatchDenied/context). The gate guarantees
	// that forged context/session args never reach that binding.
}

// A permission grant scoped to one session (its context) does not
// authorize the same action under another session/context.
func TestGrantScopeDoesNotCrossContexts(t *testing.T) {
	h := newGateHarness(t)
	if err := h.broker.GrantStanding("browser", "browser_click", "session:sess-work", false); err != nil {
		t.Fatal(err)
	}
	allow := h.broker.Evaluate("browser", "browser_click", "session:sess-work", permissions.RiskConsequential)
	if allow != permissions.VerdictAllow {
		t.Fatalf("scoped grant must allow its own session: %s", allow)
	}
	other := h.broker.Evaluate("browser", "browser_click", "session:sess-pers", permissions.RiskConsequential)
	if other == permissions.VerdictAllow {
		t.Fatal("session-scoped grant must not authorize a different session")
	}
	owner := h.broker.Evaluate("browser", "browser_click", "owner", permissions.RiskConsequential)
	if owner == permissions.VerdictAllow {
		t.Fatal("session-scoped grant must not widen to owner scope")
	}
}
