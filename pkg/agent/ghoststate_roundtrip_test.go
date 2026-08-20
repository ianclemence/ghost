package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/personalcontext"
)

// exportImportWorkspace round-trips a workspace's Ghost State into a fresh
// target workspace (passphrase-gated archive) and returns the target.
func exportImportWorkspace(t *testing.T, sourceWS string) string {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "ghost.ghost")
	if _, err := ghoststate.Export(ghoststate.ExportOptions{
		Workspace:   sourceWS,
		ConfigPath:  filepath.Join(t.TempDir(), "config.json"),
		Destination: archive,
		Passphrase:  "slice-8 test passphrase",
	}); err != nil {
		t.Fatalf("Export: %v", err)
	}
	targetWS := t.TempDir()
	if _, err := ghoststate.Import(ghoststate.ImportOptions{
		Workspace:  targetWS,
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Source:     archive,
		Passphrase: "slice-8 test passphrase",
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}
	return targetWS
}

// Imported Personal Context behaves exactly like live context: /context shows
// the current corrected belief (not the superseded one), context_get and the
// Active Context Digest serve the same value, and no new entries are generated
// by the import itself.
func TestGhostStateRoundTripPersonalContextLive(t *testing.T) {
	// Source: declare blue, correct to green, and forget a declared name.
	srcWS := newDigestWorkspace(t)
	src := newTestAgentLoopWithProvider(t, srcWS, &countingProvider{})
	runTurn(t, src, "my favorite color is blue", "s1")
	runTurn(t, src, "actually, it's green", "s1")
	runTurn(t, src, "my name is Ian", "s1")
	if resp := runTurn(t, src, "/forget name", "s1"); !strings.Contains(resp, "Forgotten: identity/name") {
		t.Fatalf("unexpected /forget response: %q", resp)
	}

	targetWS := exportImportWorkspace(t, srcWS)

	// The imported file is directly loadable by the store with no conversion.
	store, err := personalcontext.Open(targetWS)
	if err != nil {
		t.Fatalf("open imported store: %v", err)
	}

	// Lifecycle survived: green current, blue superseded, name rejected.
	var green, blue, name *personalcontext.Entry
	for i := range store.All() {
		e := store.All()[i]
		switch e.Predicate {
		case "preference/favorite_color":
			var v string
			if err := e.ValueInto(&v); err == nil {
				switch v {
				case "green":
					green = &e
				case "blue":
					blue = &e
				}
			}
		case "identity/name":
			name = &e
		}
	}
	if green == nil || green.Status != personalcontext.StatusCurrent {
		t.Fatalf("imported green = %+v, want current", green)
	}
	if blue == nil || blue.Status != personalcontext.StatusSuperseded {
		t.Fatalf("imported blue = %+v, want superseded", blue)
	}
	if blue.SupersededBy == nil || *blue.SupersededBy != green.ID {
		t.Fatalf("imported superseded_by = %v, want %s", blue.SupersededBy, green.ID)
	}
	if name == nil || name.Status != personalcontext.StatusRejected {
		t.Fatalf("imported name = %+v, want rejected", name)
	}

	// Import itself generated nothing: the imported store holds exactly the
	// entries the source store held.
	srcStore, err := personalcontext.Open(srcWS)
	if err != nil {
		t.Fatalf("open source store: %v", err)
	}
	if len(store.All()) != len(srcStore.All()) {
		t.Fatalf("imported %d entries, want %d (no import-time generation)", len(store.All()), len(srcStore.All()))
	}

	// A fresh agent on the imported workspace serves the migrated context.
	al := newTestAgentLoopWithProvider(t, targetWS, &countingProvider{})

	resp := runTurn(t, al, "/context", "s1")
	if !strings.Contains(resp, "- favorite_color: green") {
		t.Fatalf("/context missing green after import:\n%s", resp)
	}
	if strings.Contains(resp, "- favorite_color: blue") {
		t.Fatalf("/context still shows superseded blue after import:\n%s", resp)
	}

	tool, ok := al.tools.Get("context_get")
	if !ok {
		t.Fatal("context_get tool not registered")
	}
	res := tool.Execute(context.Background(), map[string]interface{}{"predicate": "preference/favorite_color"})
	if !strings.Contains(res.ForLLM, "green") || !strings.Contains(res.ForLLM, `"count":1`) {
		t.Fatalf("context_get after import = %s, want green", res.ForLLM)
	}
	res = tool.Execute(context.Background(), map[string]interface{}{"predicate": "identity/name"})
	if !strings.Contains(res.ForLLM, `"count":0`) {
		t.Fatalf("context_get after import still returns the forgotten name: %s", res.ForLLM)
	}

	// The Active Context Digest carries the imported current value.
	provider := &recordingDigestProvider{}
	al2 := newTestAgentLoopWithProvider(t, targetWS, provider)
	runTurn(t, al2, "hello", "s1")
	if !strings.Contains(provider.lastSystemPrompt, "- Favorite color: green") {
		t.Fatalf("digest after import missing green:\n%s", provider.lastSystemPrompt)
	}
	if strings.Contains(provider.lastSystemPrompt, "- Favorite color: blue") {
		t.Fatalf("digest after import shows superseded blue:\n%s", provider.lastSystemPrompt)
	}

	// The forgotten entry stays out of Current() but remains in All()/History().
	if cur := store.Current(); len(cur) != 1 {
		t.Fatalf("imported Current() = %d entries, want 1 (only green)", len(cur))
	}
	if all := store.All(); len(all) != 3 {
		t.Fatalf("imported All() = %d entries, want 3 (blue, green, name)", len(all))
	}
	if hist := store.History(name.ID); len(hist) != 2 {
		t.Fatalf("imported name history = %d revisions, want 2 (current + rejected)", len(hist))
	}
}

// Conversations and Personal Context migrate together: the imported workspace
// still serves the same session evidence alongside the migrated context.
func TestGhostStateRoundTripConversationsAlongsideContext(t *testing.T) {
	srcWS := newDigestWorkspace(t)
	src := newTestAgentLoopWithProvider(t, srcWS, &countingProvider{})
	runTurn(t, src, "my favorite color is blue", "s1")

	targetWS := exportImportWorkspace(t, srcWS)

	al := newTestAgentLoopWithProvider(t, targetWS, &countingProvider{})
	history := al.sessions.GetHistory("s1")
	var found bool
	for _, m := range history {
		if strings.Contains(m.Content, "my favorite color is blue") {
			found = true
		}
	}
	if !found {
		t.Fatal("conversation evidence did not migrate alongside Personal Context")
	}

	store, err := personalcontext.Open(targetWS)
	if err != nil {
		t.Fatalf("open imported store: %v", err)
	}
	if cur := store.Current(); len(cur) != 1 {
		t.Fatalf("imported context = %d entries, want 1", len(cur))
	}
}
