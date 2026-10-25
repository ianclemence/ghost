package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/channels"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/providers"
)

// A loop with the production rule, unlike newTestAgentLoop, which trusts every
// session so memory can be tested across many of them.
func productionRuleLoop(t *testing.T, ws string) *AgentLoop {
	t.Helper()
	cfg := &config.Config{Agents: config.AgentsConfig{Defaults: config.AgentDefaults{
		Workspace: ws, Model: "test-model", MaxTokens: 4096, MaxToolIterations: 10,
	}}}
	al, _ := NewAgentLoop(cfg, bus.NewMessageBus(), &mockProvider{})
	t.Cleanup(func() { al.Stop() })
	return al
}

// Test clients and probes talk to the same gateway as the owner, under a key of
// their own. What they say is not about the owner: a probe once filed a girlfriend,
// a dog and a death in the family into the live profile.
func TestOnlyTheOwnersConversationTeachesGhostAboutThem(t *testing.T) {
	ws := t.TempDir()
	al := productionRuleLoop(t, ws)

	for _, key := range []string{"memtest:req-1", "human-test:x", "care-1790757114260016963", "research-live-100e", "subagent:abc"} {
		runTurn(t, al, "my favorite color is blue", key)
	}
	if cur := openPCStore(t, ws).Current(); len(cur) != 0 {
		t.Fatalf("a session that is not the owner's wrote %d facts about the owner: %+v", len(cur), cur)
	}

	runTurn(t, al, "my favorite color is blue", channels.SharedConversationKey)
	if cur := openPCStore(t, ws).Current(); len(cur) != 1 {
		t.Fatalf("the owner's own conversation must still be remembered, got %d facts", len(cur))
	}
}

func TestMachineTurnsNeverTeachGhostAboutTheOwner(t *testing.T) {
	al := productionRuleLoop(t, t.TempDir())
	for _, key := range []string{"automation:daily", "routine:water", "heartbeat", "cron-1", "proactive:x"} {
		if al.learnsAboutOwner(key) {
			t.Errorf("%s is Ghost talking to itself", key)
		}
	}
	if !al.learnsAboutOwner(channels.SharedConversationKey) {
		t.Error("the shared conversation is the owner")
	}
}

func TestProbeSessionsAreNotJournaledIntoTheOwnersNotes(t *testing.T) {
	al := productionRuleLoop(t, t.TempDir())
	for _, key := range []string{"memtest:req-1", "research-live-100e", "care-1790757114260016963", "human-test:x"} {
		if al.journalsSession(key) {
			t.Errorf("%s is a probe; its test persona would land in the owner's daily notes", key)
		}
	}
	for _, key := range []string{channels.SharedConversationKey, "automation:daily", "routine:water", "heartbeat", "proactive:x"} {
		if !al.journalsSession(key) {
			t.Errorf("%s is the owner's or Ghost's own work and must still be journaled", key)
		}
	}
}

func TestThePromptNamesEveryStoreGhostKeepsAboutTheOwner(t *testing.T) {
	section := buildOwnerRecordSection()
	for _, want := range []string{"Personal Context", "/forget", "Daily journals", "does not score the tone", "keeps no mood or affinity total", "never say a mechanism does not exist"} {
		if !strings.Contains(strings.ToLower(section), strings.ToLower(want)) {
			t.Errorf("the owner-record section must mention %q", want)
		}
	}
}

// An answer to "what do you infer about me" is generated analysis. If the
// summary of that turn records it as what happened, it comes back later as a
// memory, and Ghost has then confused its own reading with a record.
func TestSummariesDoNotRecordJudgmentsAboutTheOwner(t *testing.T) {
	p := summarizePrompt(nil, "", time.Now())
	for _, want := range []string{"Do not record judgments about the owner's personality", "assistant's own analysis", "readings, not facts"} {
		if !strings.Contains(p, want) {
			t.Errorf("the summary prompt must say %q", want)
		}
	}
}

// Nothing scores the owner's tone and nothing tells the model how close it is
// to them; any such file left on disk is deleted when Ghost starts.
func TestStartingGhostDeletesTheOldRelationalScore(t *testing.T) {
	ws := t.TempDir()
	old := filepath.Join(ws, "personal-context", "affect.json")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte(`{"valence":-0.36,"affinity":0,"turns":1653}`), 0o600); err != nil {
		t.Fatal(err)
	}
	al := productionRuleLoop(t, ws)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("the old mood and affinity file survived startup")
	}
	runTurn(t, al, "I love this, amazing work, thank you!", channels.SharedConversationKey)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("a turn recreated the relational score")
	}
	prompt := al.contextBuilder.BuildSystemPrompt(nil)
	if strings.Contains(prompt, "Relational state: affinity") {
		t.Fatal("the model is still told an affinity number")
	}
}

// Asking Ghost to show what it knows reads the record and changes nothing.
// Journaling that turn files the model's commentary on the owner as a memory.
func TestReadingBackTheOwnerRecordIsNotJournaled(t *testing.T) {
	cases := []struct {
		used []string
		want bool
	}{
		{[]string{"context_get", "memory_recall", "session_search"}, true},
		{[]string{"find_tools", "context_get"}, true},
		{[]string{"context_get", "web_search"}, false},
		{[]string{"context_get", "remember"}, false},
		{[]string{"exec"}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := onlyReadBackTheOwnerRecord(c.used); got != c.want {
			t.Errorf("onlyReadBackTheOwnerRecord(%v) = %v, want %v", c.used, got, c.want)
		}
	}
}

func TestTurnToolNamesReadsOnlyTheLatestTurn(t *testing.T) {
	call := func(names ...string) providers.Message {
		m := providers.Message{Role: "assistant"}
		for _, n := range names {
			m.ToolCalls = append(m.ToolCalls, providers.ToolCall{Name: n})
		}
		return m
	}
	history := []providers.Message{
		{Role: "user", Content: "build me a page"}, call("canvas", "write_file"), {Role: "tool"}, {Role: "assistant", Content: "done"},
		{Role: "user", Content: "what do you know about me"}, call("context_get"), {Role: "tool"}, call("memory_recall"), {Role: "tool"}, {Role: "assistant", Content: "here"},
	}
	got := turnToolNames(history)
	if !onlyReadBackTheOwnerRecord(got) || len(got) != 2 {
		t.Fatalf("the latest turn only read the record, got %v", got)
	}
	if onlyReadBackTheOwnerRecord(turnToolNames(history[:4])) {
		t.Fatal("a turn that built a page is not a read-back")
	}
}
