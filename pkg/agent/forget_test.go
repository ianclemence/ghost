package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/personalcontext"
)

// /forget is registered as a normal slash command and available through the
// real agent command executor.
func TestForgetCommandRegistered(t *testing.T) {
	ws := newDigestWorkspace(t)
	al := newTestAgentLoopWithProvider(t, ws, &countingProvider{})

	if _, ok := al.commands.Lookup("/forget"); !ok {
		t.Fatal("/forget not registered in the agent command registry")
	}

	resp := runTurn(t, al, "/forget favorite_color", "s1")
	if !strings.Contains(resp, "No current Personal Context entry matches") {
		t.Fatalf("unexpected /forget response: %q", resp)
	}
}

// Full path: a declared belief flows into the next turn's digest, /forget
// retires it, and the digest no longer surfaces it.
func TestForgetEndToEndDigest(t *testing.T) {
	ws := newDigestWorkspace(t)
	provider := &recordingDigestProvider{}
	al := newTestAgentLoopWithProvider(t, ws, provider)

	runTurn(t, al, "my favorite color is blue", "s1")
	runTurn(t, al, "what's my favorite color?", "s1")
	if !strings.Contains(provider.lastSystemPrompt, "- Favorite color: blue") {
		t.Fatalf("digest missing declared fact before forget:\n%s", provider.lastSystemPrompt)
	}

	resp := runTurn(t, al, "/forget favorite_color", "s1")
	if !strings.Contains(resp, "Forgotten: preference/favorite_color") {
		t.Fatalf("unexpected /forget response: %q", resp)
	}

	runTurn(t, al, "hello", "s1")
	if strings.Contains(provider.lastSystemPrompt, "- Favorite color: blue") {
		t.Fatalf("digest still shows forgotten fact:\n%s", provider.lastSystemPrompt)
	}
}

// /forget is reflected in /context: the belief disappears from the compact
// view and no unresolved state remains.
func TestForgetEndToEndContext(t *testing.T) {
	ws := newDigestWorkspace(t)
	al := newTestAgentLoopWithProvider(t, ws, &countingProvider{})

	runTurn(t, al, "my favorite color is blue", "s1")
	resp := runTurn(t, al, "/context", "s1")
	if !strings.Contains(resp, "- favorite_color: blue") {
		t.Fatalf("/context missing blue before forget:\n%s", resp)
	}

	runTurn(t, al, "/forget favorite_color", "s1")
	resp = runTurn(t, al, "/context", "s1")
	if strings.Contains(resp, "favorite_color") {
		t.Fatalf("/context still shows forgotten belief:\n%s", resp)
	}
}

// A normal /forget never touches conversation evidence: the session transcript
// still contains the original message.
func TestForgetConversationRemains(t *testing.T) {
	ws := newDigestWorkspace(t)
	al := newTestAgentLoopWithProvider(t, ws, &countingProvider{})

	runTurn(t, al, "my favorite color is blue", "s1")
	runTurn(t, al, "/forget favorite_color", "s1")

	history := al.sessions.GetHistory("s1")
	var found bool
	for _, m := range history {
		if strings.Contains(m.Content, "my favorite color is blue") {
			found = true
		}
	}
	if !found {
		t.Fatal("original message missing from session after /forget; conversation evidence was touched")
	}
}

// The context_get tool excludes forgotten entries: after /forget, a structural
// lookup for the predicate returns no current entry.
func TestForgetContextGetExcludes(t *testing.T) {
	ws := newDigestWorkspace(t)
	al := newTestAgentLoopWithProvider(t, ws, &countingProvider{})

	runTurn(t, al, "my favorite color is blue", "s1")
	tool, ok := al.tools.Get("context_get")
	if !ok {
		t.Fatal("context_get tool not registered")
	}
	res := tool.Execute(context.Background(), map[string]interface{}{
		"predicate": "preference/favorite_color",
	})
	if !strings.Contains(res.ForLLM, `"count":1`) {
		t.Fatalf("context_get should find the entry before forget: %s", res.ForLLM)
	}

	runTurn(t, al, "/forget favorite_color", "s1")
	res = tool.Execute(context.Background(), map[string]interface{}{
		"predicate": "preference/favorite_color",
	})
	if !strings.Contains(res.ForLLM, `"count":0`) {
		t.Fatalf("context_get still returns the forgotten entry: %s", res.ForLLM)
	}
}

// /forget executes without invoking an LLM: the provider call count is
// unchanged by the command turn.
func TestForgetNoLLM(t *testing.T) {
	ws := newDigestWorkspace(t)
	provider := &countingProvider{}
	al := newTestAgentLoopWithProvider(t, ws, provider)

	runTurn(t, al, "my favorite color is blue", "s1")
	callsAfterTurn := provider.calls

	resp := runTurn(t, al, "/forget favorite_color", "s1")
	if provider.calls != callsAfterTurn {
		t.Fatalf("provider called %d times during /forget, want %d (no LLM)", provider.calls, callsAfterTurn)
	}
	if !strings.Contains(resp, "Forgotten: preference/favorite_color") {
		t.Fatalf("unexpected response: %q", resp)
	}
}

// Full path for /forget session: a declared belief's provenance references the
// session; deleting the session retires the entry and removes the evidence.
func TestForgetSessionEndToEnd(t *testing.T) {
	ws := newDigestWorkspace(t)
	al := newTestAgentLoopWithProvider(t, ws, &countingProvider{})

	runTurn(t, al, "my favorite color is blue", "s1")
	store := openPCStore(t, ws)
	cur := store.Current()
	if len(cur) != 1 {
		t.Fatalf("current entries = %d, want 1", len(cur))
	}
	if !strings.HasPrefix(cur[0].Sources[0].Ref, "s1:") {
		t.Fatalf("provenance ref = %q, want s1:<message id>", cur[0].Sources[0].Ref)
	}

	resp := runTurn(t, al, "/forget session s1", "s1")
	if !strings.Contains(resp, "Deleted session \"s1\"") {
		t.Fatalf("unexpected response: %q", resp)
	}

	store = openPCStore(t, ws)
	if all := store.All(); len(all) != 1 || all[0].Status != personalcontext.StatusRejected {
		t.Fatalf("dependent entry not retired after session deletion: %+v", all)
	}
	if hist := al.sessions.GetHistory("s1"); len(hist) != 0 {
		t.Fatalf("session evidence not deleted, %d messages remain", len(hist))
	}
}
