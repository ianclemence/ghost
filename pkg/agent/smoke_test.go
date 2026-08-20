package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/personalcontext"
)

// This file is the operational smoke test for Personal Context v1: it drives
// the real agent + command + tool + session + Ghost State interfaces against a
// temporary isolated workspace (a mock provider stands in for the LLM, so no
// model service is required — Personal Context lifecycle operations must never
// need one). It intentionally mirrors how a live installation behaves rather
// than testing store methods in isolation.

func smokeAgent(t *testing.T) (*AgentLoop, *recordingDigestProvider) {
	t.Helper()
	ws := newDigestWorkspace(t)
	provider := &recordingDigestProvider{}
	return newTestAgentLoopWithProvider(t, ws, provider), provider
}

func currentStrings(t *testing.T, store *personalcontext.Store) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, e := range store.Current() {
		out[e.Predicate] = pcValue(t, e)
	}
	return out
}

func readLog(t *testing.T, al *AgentLoop) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(al.cfg.Agents.Defaults.Workspace, "personal-context", "entries.jsonl"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	return data
}

func agentContextGet(t *testing.T, al *AgentLoop, args map[string]interface{}) string {
	t.Helper()
	tool, ok := al.tools.Get("context_get")
	if !ok {
		t.Fatalf("context_get not registered")
	}
	res := tool.Execute(context.Background(), args)
	if res.IsError {
		t.Fatalf("context_get error: %s", res.ForLLM)
	}
	return res.ForLLM
}

// SCENARIO 1 — BASIC MEMORY.
func TestSmokeScenario1BasicMemory(t *testing.T) {
	al, provider := smokeAgent(t)
	runTurn(t, al, "my favorite color is blue", "s1")

	history := al.sessions.GetHistory("s1")
	if len(history) != 2 {
		t.Fatalf("session history = %d messages, want 2 (user + assistant)", len(history))
	}
	if !strings.Contains(history[0].Content, "my favorite color is blue") {
		t.Fatalf("user message not persisted: %+v", history[0])
	}

	store := openPCStore(t, al.cfg.Agents.Defaults.Workspace)
	all := store.All()
	if len(all) != 1 {
		t.Fatalf("exactly one entry expected, got %d", len(all))
	}
	e := all[0]
	if e.Kind != personalcontext.KindPreference {
		t.Errorf("kind = %q, want preference", e.Kind)
	}
	if e.Predicate != "preference/favorite_color" {
		t.Errorf("predicate = %q, want preference/favorite_color", e.Predicate)
	}
	if v := pcValue(t, e); v != "blue" {
		t.Errorf("value = %q, want blue", v)
	}
	if e.Status != personalcontext.StatusCurrent {
		t.Errorf("status = %q, want current", e.Status)
	}
	if e.Confidence != 0.95 {
		t.Errorf("confidence = %v, want 0.95 (user_declared)", e.Confidence)
	}
	if len(e.Sources) != 1 || !strings.HasPrefix(e.Sources[0].Ref, "s1:") || e.Sources[0].Kind != personalcontext.SourceUserDeclared {
		t.Errorf("provenance = %+v, want s1:<turn> user_declared", e.Sources)
	}

	// Next turn's digest carries blue.
	runTurn(t, al, "hello", "s1")
	if !strings.Contains(provider.lastSystemPrompt, "- Favorite color: blue") {
		t.Errorf("digest missing blue:\n%s", provider.lastSystemPrompt)
	}

	// context_get returns blue.
	res := agentContextGet(t, al, map[string]interface{}{"predicate": "preference/favorite_color"})
	if !strings.Contains(res, "blue") || !strings.Contains(res, `"count":1`) {
		t.Errorf("context_get = %s, want blue count 1", res)
	}

	// /context displays blue.
	resp := runTurn(t, al, "/context", "s1")
	if !strings.Contains(resp, "- favorite_color: blue") {
		t.Errorf("/context missing blue:\n%s", resp)
	}
}

// SCENARIO 2 — CORRECTION.
func TestSmokeScenario2Correction(t *testing.T) {
	al, provider := smokeAgent(t)
	runTurn(t, al, "my favorite color is blue", "s1")
	runTurn(t, al, "actually, it's green", "s1")

	store := openPCStore(t, al.cfg.Agents.Defaults.Workspace)
	cur := currentStrings(t, store)
	if cur["preference/favorite_color"] != "green" {
		t.Fatalf("current favorite_color = %q, want green", cur["preference/favorite_color"])
	}

	byVal := map[string]*personalcontext.Entry{}
	for i := range store.All() {
		e := store.All()[i]
		byVal[pcValue(t, e)] = &e
	}
	blue, green := byVal["blue"], byVal["green"]
	if blue == nil || green == nil {
		t.Fatalf("blue/green missing: %+v", byVal)
	}
	if blue.Status != personalcontext.StatusSuperseded {
		t.Errorf("blue status = %q, want superseded", blue.Status)
	}
	if blue.SupersededBy == nil || *blue.SupersededBy != green.ID {
		t.Errorf("blue.superseded_by = %v, want green's id %s", blue.SupersededBy, green.ID)
	}
	if len(green.Sources) != 1 || green.Sources[0].Kind != personalcontext.SourceUserCorrected {
		t.Errorf("green provenance = %+v, want user_corrected", green.Sources)
	}
	if hist := store.History(blue.ID); len(hist) != 2 {
		t.Errorf("blue history = %d records, want 2 (blue + superseded revision)", len(hist))
	}

	// Digest: green current, blue never presented as current.
	runTurn(t, al, "hello", "s1")
	if !strings.Contains(provider.lastSystemPrompt, "- Favorite color: green") {
		t.Errorf("digest missing green:\n%s", provider.lastSystemPrompt)
	}
	if strings.Contains(provider.lastSystemPrompt, "- Favorite color: blue") {
		t.Errorf("digest presents superseded blue:\n%s", provider.lastSystemPrompt)
	}

	res := agentContextGet(t, al, map[string]interface{}{"predicate": "preference/favorite_color"})
	if !strings.Contains(res, "green") || strings.Contains(res, `"blue"`) {
		t.Errorf("context_get = %s, want green only", res)
	}
	resp := runTurn(t, al, "/context", "s1")
	if !strings.Contains(resp, "- favorite_color: green") || strings.Contains(resp, "- favorite_color: blue") {
		t.Errorf("/context wrong after correction:\n%s", resp)
	}
}

// SCENARIO 3 — RESTART.
func TestSmokeScenario3Restart(t *testing.T) {
	ws := newDigestWorkspace(t)
	al1 := newTestAgentLoopWithProvider(t, ws, &recordingDigestProvider{})
	runTurn(t, al1, "my favorite color is blue", "s1")
	runTurn(t, al1, "actually, it's green", "s1")
	if got := len(openPCStore(t, ws).All()); got != 2 {
		t.Fatalf("pre-restart entries = %d, want 2 (blue superseded + green current)", got)
	}

	// Restart: a brand-new agent on the same workspace.
	provider2 := &recordingDigestProvider{}
	al2 := newTestAgentLoopWithProvider(t, ws, provider2)

	store := openPCStore(t, ws)
	if got := len(store.All()); got != 2 {
		t.Fatalf("post-restart entries = %d, want 2 (restart must not generate entries)", got)
	}
	cur := currentStrings(t, store)
	if cur["preference/favorite_color"] != "green" {
		t.Fatalf("after restart current = %q, want green", cur["preference/favorite_color"])
	}

	runTurn(t, al2, "hello", "s1")
	if !strings.Contains(provider2.lastSystemPrompt, "- Favorite color: green") {
		t.Errorf("digest after restart missing green:\n%s", provider2.lastSystemPrompt)
	}
	res := agentContextGet(t, al2, map[string]interface{}{"predicate": "preference/favorite_color"})
	if !strings.Contains(res, "green") {
		t.Errorf("context_get after restart = %s, want green", res)
	}
	if resp := runTurn(t, al2, "/context", "s1"); !strings.Contains(resp, "- favorite_color: green") {
		t.Errorf("/context after restart wrong:\n%s", resp)
	}
}

// SCENARIO 4 — FORGET (including repeated forget idempotency).
func TestSmokeScenario4Forget(t *testing.T) {
	al, provider := smokeAgent(t)
	runTurn(t, al, "my favorite color is blue", "s1")
	runTurn(t, al, "hello", "s1")
	if !strings.Contains(provider.lastSystemPrompt, "- Favorite color: blue") {
		t.Fatalf("pre-forget digest missing blue:\n%s", provider.lastSystemPrompt)
	}

	resp := runTurn(t, al, "/forget favorite_color", "s1")
	if !strings.Contains(resp, "Forgotten: preference/favorite_color") {
		t.Fatalf("unexpected /forget response: %q", resp)
	}

	store := openPCStore(t, al.cfg.Agents.Defaults.Workspace)
	all := store.All()
	if len(all) != 1 || all[0].Status != personalcontext.StatusRejected {
		t.Fatalf("after forget: %+v, want one rejected entry", all)
	}
	if hist := store.History(all[0].ID); len(hist) != 2 {
		t.Fatalf("history = %d records, want 2 (current + rejected)", len(hist))
	}

	// The original conversation evidence is untouched by a normal forget.
	history := al.sessions.GetHistory("s1")
	if !strings.Contains(history[0].Content, "my favorite color is blue") {
		t.Error("conversation evidence was deleted by a normal /forget")
	}

	// Repeated forget: no duplicate rejection revision, no corruption.
	resp2 := runTurn(t, al, "/forget favorite_color", "s1")
	if !strings.Contains(resp2, "already forgotten") {
		t.Fatalf("second /forget response = %q, want 'already forgotten'", resp2)
	}
	if hist := store.History(all[0].ID); len(hist) != 2 {
		t.Fatalf("after repeated forget history = %d records, want still 2", len(hist))
	}
	if got := strings.Count(string(readLog(t, al)), "\n"); got != 2 {
		t.Fatalf("log lines = %d, want 2 (no extra revision appended)", got)
	}

	// All current-facing surfaces agree.
	runTurn(t, al, "hello", "s1")
	if strings.Contains(provider.lastSystemPrompt, "- Favorite color: blue") {
		t.Errorf("digest still shows forgotten value:\n%s", provider.lastSystemPrompt)
	}
	res := agentContextGet(t, al, map[string]interface{}{"predicate": "preference/favorite_color"})
	if !strings.Contains(res, `"count":0`) {
		t.Errorf("context_get after forget = %s, want count 0", res)
	}
	if resp := runTurn(t, al, "/context", "s1"); strings.Contains(resp, "favorite_color") {
		t.Errorf("/context after forget still shows the belief:\n%s", resp)
	}
}

// SCENARIO 5 — GHOST STATE ROUND TRIP.
func TestSmokeScenario5GhostStateRoundTrip(t *testing.T) {
	ws := newDigestWorkspace(t)
	al := newTestAgentLoopWithProvider(t, ws, &countingProvider{})
	runTurn(t, al, "my favorite color is blue", "s1")
	runTurn(t, al, "actually, it's green", "s1")

	targetWS := exportImportWorkspace(t, ws)

	// Conversations are present in the fresh workspace.
	al2 := newTestAgentLoopWithProvider(t, targetWS, &countingProvider{})
	if hist := al2.sessions.GetHistory("s1"); len(hist) == 0 {
		t.Fatal("conversations missing after import")
	}

	// Personal Context present, lifecycle and provenance intact, nothing
	// regenerated by import.
	store := openPCStore(t, targetWS)
	srcStore := openPCStore(t, ws)
	if len(store.All()) != len(srcStore.All()) {
		t.Fatalf("imported %d entries, source %d (import must not extract)", len(store.All()), len(srcStore.All()))
	}
	cur := currentStrings(t, store)
	if cur["preference/favorite_color"] != "green" {
		t.Fatalf("imported current = %q, want green", cur["preference/favorite_color"])
	}
	var blue *personalcontext.Entry
	for i := range store.All() {
		if e := store.All()[i]; pcValue(t, e) == "blue" {
			blue = &e
		}
	}
	if blue == nil || blue.Status != personalcontext.StatusSuperseded || blue.SupersededBy == nil {
		t.Fatalf("imported blue = %+v, want superseded with superseded_by", blue)
	}
	if len(blue.Sources) == 0 || !strings.HasPrefix(blue.Sources[0].Ref, "s1:") {
		t.Errorf("imported blue provenance lost: %+v", blue.Sources)
	}

	// All surfaces work immediately, no RAG index involved.
	provider3 := &recordingDigestProvider{}
	al3 := newTestAgentLoopWithProvider(t, targetWS, provider3)
	runTurn(t, al3, "hello", "s1")
	if !strings.Contains(provider3.lastSystemPrompt, "- Favorite color: green") {
		t.Errorf("digest after import missing green:\n%s", provider3.lastSystemPrompt)
	}
	res := agentContextGet(t, al3, map[string]interface{}{"predicate": "preference/favorite_color"})
	if !strings.Contains(res, "green") {
		t.Errorf("context_get after import = %s, want green", res)
	}
	if resp := runTurn(t, al3, "/context", "s1"); !strings.Contains(resp, "- favorite_color: green") || strings.Contains(resp, "- favorite_color: blue") {
		t.Errorf("/context after import wrong:\n%s", resp)
	}
}

// SCENARIO 6 — CONFLICT (constructed through the supported store API).
func TestSmokeScenario6Conflict(t *testing.T) {
	al, provider := smokeAgent(t)
	if al.pcStore == nil {
		t.Fatal("agent has no personal context store")
	}
	store := al.pcStore

	now := time.Now().UTC()
	mk := func(id, value string) personalcontext.Entry {
		return personalcontext.Entry{
			ID:         id,
			Kind:       personalcontext.KindFact,
			Subject:    "user",
			Predicate:  "fact/conflict_test",
			Value:      mustPCValue(t, value),
			Status:     personalcontext.StatusCurrent,
			Confidence: 0.5,
			Sources: []personalcontext.Source{{
				Type:      personalcontext.SourceAgentInference,
				Kind:      personalcontext.SourceInferred,
				Ref:       "s1:m1",
				Timestamp: now,
			}},
			CreatedAt: now,
			UpdatedAt: now,
		}
	}
	if _, err := store.Create(mk("conf_a", "A")); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if _, err := store.Create(mk("conf_b", "B")); err != nil {
		t.Fatalf("create B: %v", err)
	}
	if err := store.DeclareConflict("user", "fact/conflict_test", "conf_a", "conf_b"); err != nil {
		t.Fatalf("DeclareConflict: %v", err)
	}

	// Neither conflicting value is a current fact.
	if cur := store.Current(); len(cur) != 0 {
		t.Fatalf("conflicting values present as current: %+v", cur)
	}

	// context_get surfaces unresolved state, not facts.
	res := agentContextGet(t, al, map[string]interface{}{"predicate": "fact/conflict_test"})
	if !strings.Contains(res, `"count":0`) {
		t.Errorf("context_get presented a conflicting value as current: %s", res)
	}
	if !strings.Contains(res, `"unresolved"`) {
		t.Errorf("context_get missing unresolved state: %s", res)
	}
	if !strings.Contains(res, "do not treat them as facts") {
		t.Errorf("context_get unresolved note missing: %s", res)
	}

	// /context surfaces the conflict under Unresolved, never as current.
	resp := runTurn(t, al, "/context", "s1")
	if !strings.Contains(resp, "Unresolved") || !strings.Contains(resp, "fact/conflict_test") {
		t.Errorf("/context missing unresolved section:\n%s", resp)
	}
	if !strings.Contains(resp, "No current beliefs") {
		t.Errorf("/context presented a conflicting value as current:\n%s", resp)
	}

	// The digest excludes conflicting values entirely.
	runTurn(t, al, "hello", "s1")
	if strings.Contains(provider.lastSystemPrompt, "conflict_test") {
		t.Errorf("conflicting values leaked into digest:\n%s", provider.lastSystemPrompt)
	}

	// No component silently selected one side.
	byVal := map[string]string{}
	for _, e := range store.All() {
		byVal[pcValue(t, e)] = string(e.Status)
	}
	if byVal["A"] != "conflicting" || byVal["B"] != "conflicting" {
		t.Fatalf("conflict not recorded on both sides: %+v", byVal)
	}
}

// SCENARIO 7 — TEMPORAL VALIDITY (constructed via the supported store API).
func TestSmokeScenario7TemporalValidity(t *testing.T) {
	ws := newDigestWorkspace(t)
	al := newTestAgentLoopWithProvider(t, ws, &countingProvider{})
	store := openPCStore(t, ws)

	now := time.Now().UTC()
	past := now.Add(-48 * time.Hour)
	future := now.Add(48 * time.Hour)
	windowStart := now.Add(-24 * time.Hour)
	windowEnd := now.Add(24 * time.Hour)

	mk := func(id, pred, value string) personalcontext.Entry {
		return personalcontext.Entry{
			ID:         id,
			Kind:       personalcontext.KindFact,
			Subject:    "user",
			Predicate:  pred,
			Value:      mustPCValue(t, value),
			Status:     personalcontext.StatusCurrent,
			Confidence: 0.95,
			Sources: []personalcontext.Source{{
				Type:      personalcontext.SourceConversation,
				Kind:      personalcontext.SourceUserDeclared,
				Ref:       "s1:m1",
				Timestamp: now,
			}},
			CreatedAt: now,
			UpdatedAt: now,
		}
	}

	futureE := mk("t_future", "fact/future_test", "FUTURE")
	futureE.ValidFrom = &future
	expiredE := mk("t_expired", "fact/expired_test", "EXPIRED")
	expiredE.ValidUntil = &past
	validE := mk("t_valid", "fact/valid_test", "VALID")
	validE.ValidFrom = &windowStart
	validE.ValidUntil = &windowEnd
	for _, e := range []personalcontext.Entry{futureE, expiredE, validE} {
		if _, err := store.Create(e); err != nil {
			t.Fatalf("create %s: %v", e.ID, err)
		}
	}

	verify := func(t *testing.T, al *AgentLoop, provider *recordingDigestProvider, label string) {
		t.Helper()
		cur := currentStrings(t, openPCStore(t, al.cfg.Agents.Defaults.Workspace))
		if _, ok := cur["fact/future_test"]; ok {
			t.Errorf("%s: future entry is current", label)
		}
		if _, ok := cur["fact/expired_test"]; ok {
			t.Errorf("%s: expired entry is current", label)
		}
		if cur["fact/valid_test"] != "VALID" {
			t.Errorf("%s: valid-window entry missing: %+v", label, cur)
		}

		for pred, wantCurrent := range map[string]bool{
			"fact/future_test":  false,
			"fact/expired_test": false,
			"fact/valid_test":   true,
		} {
			res := agentContextGet(t, al, map[string]interface{}{"predicate": pred})
			if got := strings.Contains(res, `"count":1`); got != wantCurrent {
				t.Errorf("%s: context_get(%s) current=%v, want %v: %s", label, pred, got, wantCurrent, res)
			}
		}

		resp := runTurn(t, al, "/context", "s1")
		if strings.Contains(resp, "future_test") || strings.Contains(resp, "expired_test") {
			t.Errorf("%s: /context shows non-current entry:\n%s", label, resp)
		}
		if !strings.Contains(resp, "- valid_test: VALID") {
			t.Errorf("%s: /context missing valid entry:\n%s", label, resp)
		}

		runTurn(t, al, "hello", "s1")
		if strings.Contains(provider.lastSystemPrompt, "FUTURE") || strings.Contains(provider.lastSystemPrompt, "EXPIRED") {
			t.Errorf("%s: digest shows non-current entry:\n%s", label, provider.lastSystemPrompt)
		}
		if !strings.Contains(provider.lastSystemPrompt, "- valid test: VALID") {
			t.Errorf("%s: digest missing valid entry:\n%s", label, provider.lastSystemPrompt)
		}
	}

	provider := &recordingDigestProvider{}
	al = newTestAgentLoopWithProvider(t, ws, provider)
	verify(t, al, provider, "live")

	// Restart and repeat: same semantics, same store state.
	provider2 := &recordingDigestProvider{}
	al2 := newTestAgentLoopWithProvider(t, ws, provider2)
	verify(t, al2, provider2, "after-restart")
}

// SCENARIO 8 — MULTIPLE FACTS (distinct predicates, then the same predicate).
func TestSmokeScenario8MultipleFacts(t *testing.T) {
	al := newTestAgentLoopWithProvider(t, newDigestWorkspace(t), &countingProvider{})

	// Distinct predicates in one message extract cleanly.
	runTurn(t, al, "My favorite color is blue and I prefer concise answers.", "s1")
	store := openPCStore(t, al.cfg.Agents.Defaults.Workspace)
	cur := currentStrings(t, store)
	if cur["preference/favorite_color"] != "blue" {
		t.Errorf("favorite_color = %q, want blue", cur["preference/favorite_color"])
	}
	if cur["preference/communication.style"] != "concise" {
		t.Errorf("communication.style = %q, want concise", cur["preference/communication.style"])
	}

	// The hardening fix: same predicate twice in one message -> later wins,
	// exactly one current entry, earlier revision preserved in history.
	runTurn(t, al, "my goal is to launch X and I want to build Y", "s1")
	store = openPCStore(t, al.cfg.Agents.Defaults.Workspace)
	cur = currentStrings(t, store)
	if cur["goal/primary"] != "build Y" {
		t.Fatalf("goal/primary = %q, want the later declaration build Y", cur["goal/primary"])
	}
	goalCount := 0
	for _, e := range store.All() {
		if e.Predicate != "goal/primary" {
			continue
		}
		goalCount++
		if pcValue(t, e) == "launch X" && e.Status != personalcontext.StatusSuperseded {
			t.Errorf("earlier goal revision not superseded: %+v", e)
		}
	}
	if goalCount != 2 {
		t.Fatalf("goal/primary records = %d, want 2 (earlier + later)", goalCount)
	}
}

// SCENARIO 9 — ORDINARY CONVERSATION: no false memories.
func TestSmokeScenario9OrdinaryConversation(t *testing.T) {
	al, provider := smokeAgent(t)
	store := openPCStore(t, al.cfg.Agents.Defaults.Workspace)

	for _, msg := range []string{
		"I had coffee this morning.",
		"Please help me debug this function.",
		"What time is it?",
		"thanks!",
		"That's great news.",
	} {
		runTurn(t, al, msg, "s1")
	}
	if all := store.All(); len(all) != 0 {
		t.Fatalf("ordinary conversation created %d Personal Context entries: %+v", len(all), all)
	}
	if count := strings.Count(provider.lastSystemPrompt, "<personal_context>"); count != 0 {
		t.Fatalf("digest present without any context:\n%s", provider.lastSystemPrompt)
	}
}

// SCENARIO 10 — FAILURE ISOLATION.
func TestSmokeScenario10FailureIsolation(t *testing.T) {
	// (a) Store unavailable: a file occupying the personal-context directory.
	t.Run("store-unavailable", func(t *testing.T) {
		ws := t.TempDir()
		if err := os.WriteFile(filepath.Join(ws, "personal-context"), []byte("blocked"), 0644); err != nil {
			t.Fatalf("write blocker: %v", err)
		}
		al := newTestAgentLoop(t, ws)
		if resp := runTurn(t, al, "hello", "s1"); resp == "" {
			t.Fatal("turn produced no response")
		}
		if _, ok := al.tools.Get("context_get"); ok {
			t.Error("context_get should not exist when the store is unavailable")
		}
		if resp := runTurn(t, al, "/context", "s1"); !strings.Contains(resp, "Personal Context is unavailable.") {
			t.Errorf("/context = %q, want unavailable", resp)
		}
	})

	// (b) Store write failure: extraction fails but the turn succeeds.
	t.Run("write-failure", func(t *testing.T) {
		ws := newDigestWorkspace(t)
		al := newTestAgentLoop(t, ws)
		runTurn(t, al, "my favorite color is blue", "s1")
		logPath := filepath.Join(ws, "personal-context", "entries.jsonl")
		if err := os.Chmod(logPath, 0444); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		defer os.Chmod(logPath, 0644)
		if resp := runTurn(t, al, "actually, my favorite color is green", "s1"); resp == "" {
			t.Fatal("turn produced no response despite PC write failure")
		}
		store := openPCStore(t, ws)
		cur := currentStrings(t, store)
		if cur["preference/favorite_color"] != "blue" {
			t.Fatalf("failed write changed context: %+v", cur)
		}
	})

	// (c) Malformed Personal Context log: the agent still runs; the store
	// fails loudly rather than silently dropping entries.
	t.Run("malformed-log", func(t *testing.T) {
		ws := t.TempDir()
		dir := filepath.Join(ws, "personal-context")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "entries.jsonl"), []byte(`NOT JSON`), 0644); err != nil {
			t.Fatalf("write malformed log: %v", err)
		}
		al := newTestAgentLoop(t, ws)
		if resp := runTurn(t, al, "hello", "s1"); resp == "" {
			t.Fatal("turn produced no response with a malformed log")
		}
		if _, ok := al.tools.Get("context_get"); ok {
			t.Error("context_get should not exist when the log is malformed")
		}
		if resp := runTurn(t, al, "/context", "s1"); !strings.Contains(resp, "Personal Context is unavailable.") {
			t.Errorf("/context = %q, want unavailable", resp)
		}
	})
}

// OPERATIONAL REVIEW — read-only operations never write to the Personal
// Context log, and every surface reads the same live store instance.
func TestSmokeOperationalReview(t *testing.T) {
	al, provider := smokeAgent(t)
	runTurn(t, al, "my favorite color is blue", "s1")

	logBefore := readLog(t, al)

	// Read-only surfaces: /context, context_get, and a digest turn (ordinary
	// conversation triggers no extraction).
	runTurn(t, al, "/context", "s1")
	agentContextGet(t, al, map[string]interface{}{"predicate": "preference/favorite_color"})
	runTurn(t, al, "hello", "s1")

	logAfter := readLog(t, al)
	if string(logBefore) != string(logAfter) {
		t.Fatalf("read-only operations wrote to the log:\nbefore:\n%s\nafter:\n%s", logBefore, logAfter)
	}
	if !strings.Contains(provider.lastSystemPrompt, "- Favorite color: blue") {
		t.Errorf("digest missing blue after read-only ops:\n%s", provider.lastSystemPrompt)
	}

	// Single live store: a direct mutation through the agent's own store
	// pointer is immediately visible to /context and context_get without any
	// extraction or store re-open, proving /context, context_get, and the
	// extraction path all share one instance.
	if al.pcStore == nil {
		t.Fatal("agent has no personal context store")
	}
	if err := al.pcStore.Forget(""); err == nil {
		t.Fatal("expected an error for an empty forget id")
	}
	// Seed a second belief directly and confirm the live surfaces see it.
	if _, err := al.pcStore.Create(personalcontext.Entry{
		ID:         "op_name",
		Kind:       personalcontext.KindIdentity,
		Subject:    "user",
		Predicate:  "identity/name",
		Value:      mustPCValue(t, "Ian"),
		Status:     personalcontext.StatusCurrent,
		Confidence: 0.95,
		Sources: []personalcontext.Source{{
			Type:      personalcontext.SourceConversation,
			Kind:      personalcontext.SourceUserDeclared,
			Ref:       "s1:m1",
			Timestamp: time.Now().UTC(),
		}},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if resp := runTurn(t, al, "/context", "s1"); !strings.Contains(resp, "- name: Ian") {
		t.Errorf("/context did not see the direct store write:\n%s", resp)
	}
	res := agentContextGet(t, al, map[string]interface{}{"predicate": "identity/name"})
	if !strings.Contains(res, "Ian") {
		t.Errorf("context_get did not see the direct store write: %s", res)
	}
}
