package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/channels"
	"github.com/ianclemence/ghost/pkg/config"
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
	if al.Affect().Turns != 0 {
		t.Fatalf("a probe moved the owner's relational score: %d turns", al.Affect().Turns)
	}

	runTurn(t, al, "my favorite color is blue", channels.SharedConversationKey)
	if cur := openPCStore(t, ws).Current(); len(cur) != 1 {
		t.Fatalf("the owner's own conversation must still be remembered, got %d facts", len(cur))
	}
	if al.Affect().Turns != 1 {
		t.Fatalf("the owner's turn must count, got %d", al.Affect().Turns)
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
	for _, want := range []string{"Personal Context", "/forget", "Daily journals", "Relational state", "/affect", "warm and cold turn counts", "never say a mechanism does not exist"} {
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
