package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/personalcontext"
)

// These tests exercise the Personal Context integration boundary: the agent
// turn loop persists a user message and then runs the existing rule-based
// extractor against it. They use the real personalcontext store (JSONL in the
// workspace) and a mock provider, so no model/Ollama is required.

func newTestAgentLoop(t *testing.T, workspace string) *AgentLoop {
	t.Helper()
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         workspace,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}
	msgBus := bus.NewMessageBus()
	al, _ := NewAgentLoop(cfg, msgBus, &mockProvider{})
	t.Cleanup(func() { al.Stop() })
	return al
}

func runTurn(t *testing.T, al *AgentLoop, content, sessionKey string) string {
	t.Helper()
	ctx := context.Background()
	msg := bus.InboundMessage{
		Channel:    "test",
		SenderID:   "user1",
		ChatID:     "chat1",
		Content:    content,
		SessionKey: sessionKey,
	}
	resp, err := al.processMessage(ctx, msg, nil, nil)
	if err != nil {
		t.Fatalf("processMessage(%q): %v", content, err)
	}
	return resp
}

func openPCStore(t *testing.T, workspace string) *personalcontext.Store {
	t.Helper()
	s, err := personalcontext.Open(workspace)
	if err != nil {
		t.Fatalf("open personal context: %v", err)
	}
	return s
}

func pcValue(t *testing.T, e personalcontext.Entry) string {
	t.Helper()
	var v string
	if err := e.ValueInto(&v); err != nil {
		t.Fatalf("entry %s value %s is not a string: %v", e.ID, e.Value, err)
	}
	return v
}

// Acceptance B: an explicit declaration is persisted after the user turn.
func TestPersonalContextDeclarationPersists(t *testing.T) {
	ws := t.TempDir()
	al := newTestAgentLoop(t, ws)

	runTurn(t, al, "my favorite color is blue", "s1")

	store := openPCStore(t, ws)
	cur := store.Current()
	if len(cur) != 1 {
		t.Fatalf("current entries = %d, want 1: %+v", len(cur), cur)
	}
	e := cur[0]
	if e.Kind != personalcontext.KindPreference {
		t.Errorf("kind = %q, want preference", e.Kind)
	}
	if e.Subject != "user" {
		t.Errorf("subject = %q, want user", e.Subject)
	}
	if e.Predicate != "preference/favorite_color" {
		t.Errorf("predicate = %q, want preference/favorite_color", e.Predicate)
	}
	if got := pcValue(t, e); got != "blue" {
		t.Errorf("value = %q, want blue", got)
	}
	if e.Status != personalcontext.StatusCurrent {
		t.Errorf("status = %q, want current", e.Status)
	}
	if e.Confidence < 0.9 {
		t.Errorf("confidence = %v, want >= 0.9", e.Confidence)
	}
	if len(e.Sources) != 1 {
		t.Fatalf("sources = %d, want 1", len(e.Sources))
	}
	src := e.Sources[0]
	if src.Type != personalcontext.SourceConversation {
		t.Errorf("source type = %q, want conversation", src.Type)
	}
	if src.Kind != personalcontext.SourceUserDeclared {
		t.Errorf("source kind = %q, want user_declared", src.Kind)
	}
	if !strings.HasPrefix(src.Ref, "s1:") {
		t.Errorf("source ref = %q, want s1:<message id>", src.Ref)
	}
	if src.Timestamp.IsZero() {
		t.Error("source timestamp is zero")
	}

	if _, err := os.Stat(filepath.Join(ws, "personal-context", "entries.jsonl")); err != nil {
		t.Errorf("entries.jsonl missing after turn: %v", err)
	}
}

// Acceptance C: an explicit correction supersedes the existing entry; the old
// value is retired and the new one becomes current with user_corrected
// provenance and confidence 1.0.
func TestPersonalContextCorrectionSupersedes(t *testing.T) {
	ws := t.TempDir()
	al := newTestAgentLoop(t, ws)

	runTurn(t, al, "my favorite color is blue", "s1")
	runTurn(t, al, "actually, my favorite color is green", "s1")

	store := openPCStore(t, ws)
	cur := store.Current()
	if len(cur) != 1 {
		t.Fatalf("current entries = %d, want 1: %+v", len(cur), cur)
	}
	e := cur[0]
	if e.Predicate != "preference/favorite_color" {
		t.Errorf("predicate = %q, want preference/favorite_color", e.Predicate)
	}
	if got := pcValue(t, e); got != "green" {
		t.Errorf("value = %q, want green", got)
	}
	if e.Confidence != 1.0 {
		t.Errorf("confidence = %v, want 1.0", e.Confidence)
	}
	if e.Sources[0].Kind != personalcontext.SourceUserCorrected {
		t.Errorf("source kind = %q, want user_corrected", e.Sources[0].Kind)
	}

	var blue *personalcontext.Entry
	for i := range store.All() {
		if pcValue(t, store.All()[i]) == "blue" {
			blue = &store.All()[i]
		}
	}
	if blue == nil {
		t.Fatal("blue entry missing from the store")
	}
	if blue.Status != personalcontext.StatusSuperseded {
		t.Errorf("blue status = %q, want superseded", blue.Status)
	}
	if blue.SupersededBy == nil || *blue.SupersededBy != e.ID {
		t.Errorf("blue superseded_by = %v, want %s", blue.SupersededBy, e.ID)
	}
}

// Acceptance D: ordinary conversation produces no Personal Context entries.
func TestPersonalContextOrdinaryConversation(t *testing.T) {
	ws := t.TempDir()
	al := newTestAgentLoop(t, ws)

	runTurn(t, al, "I had coffee this morning.", "s1")

	store := openPCStore(t, ws)
	if cur := store.Current(); len(cur) != 0 {
		t.Fatalf("current entries = %d, want 0: %+v", len(cur), cur)
	}
	if all := store.All(); len(all) != 0 {
		t.Fatalf("all entries = %d, want 0", len(all))
	}
}

// A deictic correction is resolved through the immediately preceding user
// turn, which the turn loop provides as PreviousText.
func TestPersonalContextDeicticCorrectionUsesPreviousTurn(t *testing.T) {
	ws := t.TempDir()
	al := newTestAgentLoop(t, ws)

	runTurn(t, al, "my favorite color is blue", "s1")
	runTurn(t, al, "actually, it's green", "s1")

	store := openPCStore(t, ws)
	cur := store.Current()
	if len(cur) != 1 || pcValue(t, cur[0]) != "green" {
		t.Fatalf("current = %+v, want green", cur)
	}
	if cur[0].Sources[0].Kind != personalcontext.SourceUserCorrected {
		t.Errorf("source kind = %q, want user_corrected", cur[0].Sources[0].Kind)
	}
}

// Acceptance E (open): if the Personal Context store cannot open, the agent
// still starts and normal user processing succeeds with the failure logged.
func TestPersonalContextOpenFailureDoesNotBreakTurn(t *testing.T) {
	ws := t.TempDir()
	// Block the personal-context directory with a regular file so Open fails.
	if err := os.WriteFile(filepath.Join(ws, "personal-context"), []byte("blocked"), 0644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	logFile := filepath.Join(t.TempDir(), "agent.log")
	if err := logger.EnableFileLogging(logFile); err != nil {
		t.Fatalf("enable file logging: %v", err)
	}
	defer logger.DisableFileLogging()

	al := newTestAgentLoop(t, ws)
	if resp := runTurn(t, al, "my favorite color is blue", "s1"); resp == "" {
		t.Fatal("turn produced no response")
	}

	// The turn must not have created personal context.
	if _, err := os.Stat(filepath.Join(ws, "personal-context", "entries.jsonl")); err == nil {
		t.Errorf("entries.jsonl should not exist when the store failed to open")
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "Personal Context unavailable") {
		t.Errorf("expected 'Personal Context unavailable' in log:\n%s", data)
	}
}

// Acceptance E (write): if persisting an extraction fails, the normal user
// turn still succeeds and the failure is logged; the conversation is not
// rolled back.
func TestPersonalContextWriteFailureDoesNotBreakTurn(t *testing.T) {
	ws := t.TempDir()
	al := newTestAgentLoop(t, ws)

	runTurn(t, al, "my favorite color is blue", "s1")

	// Make the entries log read-only so the next extraction write fails.
	path := filepath.Join(ws, "personal-context", "entries.jsonl")
	if err := os.Chmod(path, 0444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(path, 0644)

	logFile := filepath.Join(t.TempDir(), "agent.log")
	if err := logger.EnableFileLogging(logFile); err != nil {
		t.Fatalf("enable file logging: %v", err)
	}
	defer logger.DisableFileLogging()

	if resp := runTurn(t, al, "actually, my favorite color is green", "s1"); resp == "" {
		t.Fatal("turn produced no response despite Personal Context write failure")
	}

	// The failed correction must not have been persisted.
	store := openPCStore(t, ws)
	cur := store.Current()
	if len(cur) != 1 || pcValue(t, cur[0]) != "blue" {
		t.Fatalf("current = %+v, want unchanged blue", cur)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "Personal Context extraction failed") {
		t.Errorf("expected 'Personal Context extraction failed' in log:\n%s", data)
	}
}

// Acceptance F: re-initializing the agent against the same workspace reloads
// existing Personal Context entries and new turns append correctly.
func TestPersonalContextRestartReloadsExistingEntries(t *testing.T) {
	ws := t.TempDir()
	al := newTestAgentLoop(t, ws)
	runTurn(t, al, "my favorite color is blue", "s1")

	// Re-initialize the agent on the same workspace.
	al2 := newTestAgentLoop(t, ws)
	runTurn(t, al2, "actually, my favorite color is green", "s1")

	store := openPCStore(t, ws)
	cur := store.Current()
	if len(cur) != 1 || pcValue(t, cur[0]) != "green" {
		t.Fatalf("current = %+v, want green after restart", cur)
	}
	if cur[0].Sources[0].Kind != personalcontext.SourceUserCorrected {
		t.Errorf("source kind = %q, want user_corrected", cur[0].Sources[0].Kind)
	}

	// The pre-restart blue entry survived and is superseded.
	hasSupersededBlue := false
	for _, e := range store.All() {
		if pcValue(t, e) == "blue" && e.Status == personalcontext.StatusSuperseded {
			hasSupersededBlue = true
		}
	}
	if !hasSupersededBlue {
		t.Error("superseded blue entry missing after restart")
	}
}
