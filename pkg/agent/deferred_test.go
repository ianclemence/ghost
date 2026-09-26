package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/providers"
)

// memProvider answers the personal-context classifier with one durable fact.
type memProvider struct {
	calls  int
	fail   bool
	answer string
}

func (m *memProvider) Chat(ctx context.Context, msgs []providers.Message, tools []providers.ToolDefinition, model string, opts map[string]interface{}) (*providers.LLMResponse, error) {
	m.calls++
	if m.fail {
		return nil, context.DeadlineExceeded
	}
	if m.answer != "" {
		return &providers.LLMResponse{Content: m.answer}, nil
	}
	return &providers.LLMResponse{Content: `{"should_remember":true,"kind":"preference","domain":"food","confidence":0.9,"memories":[{"kind":"preference","domain":"food","confidence":0.9,"summary":"I always order oat milk lattes"}]}`}, nil
}
func (m *memProvider) GetDefaultModel() string { return "mem-test" }

func newExtractorFor(p providers.LLMProvider, al *AgentLoop) *personalcontext.SemanticExtractor {
	return personalcontext.NewSemanticExtractor(p, "mem-test")
}

// The whole point of deferral: the reply is produced without waiting for
// extraction, and the durable record still settles afterwards.
func TestDeferredExtractionRunsAfterTheAnswer(t *testing.T) {
	al, _, _ := newProactiveLoop(t)
	p := &memProvider{}
	al.semanticExtractor = newExtractorFor(p, al)

	al.deferExtraction("main", "req-defer-1", "I always order oat milk lattes", "cli")

	jobs := al.readDeferredQueue()
	if len(jobs) != 1 || jobs[0].RequestID != "req-defer-1" {
		t.Fatalf("deferral must record the turn durably, got %+v", jobs)
	}
	if p.calls != 0 {
		t.Fatal("deferral must not call the model")
	}

	if n := al.FlushDeferred(); n != 1 {
		t.Fatalf("flush processed %d records, want 1", n)
	}
	if p.calls == 0 {
		t.Fatal("the deferred pass must reach the model")
	}
	if len(al.readDeferredQueue()) != 0 {
		t.Fatal("a processed record must be removed")
	}
	found := false
	for _, e := range al.pcStore.Current() {
		if strings.Contains(strings.ToLower(entryValueText(e)), "oat milk") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the deferred extraction must settle into memory, got %+v", al.pcStore.Current())
	}
}

// A retry of the same turn must not enqueue twice.
func TestDeferredExtractionDeduplicatesTheTurn(t *testing.T) {
	al, _, _ := newProactiveLoop(t)
	al.semanticExtractor = newExtractorFor(&memProvider{}, al)
	for i := 0; i < 3; i++ {
		al.deferExtraction("main", "req-dup", "I always order oat milk lattes", "cli")
	}
	if got := len(al.readDeferredQueue()); got != 1 {
		t.Fatalf("the same turn must be queued once, got %d", got)
	}
}

// The queue survives a restart: a fresh runtime drains what the previous
// process left behind.
func TestDeferredExtractionSurvivesRestart(t *testing.T) {
	ws := t.TempDir()
	al, _, _ := newProactiveLoopWith(t, ws, &mockProvider{})
	p := &memProvider{}
	al.semanticExtractor = newExtractorFor(p, al)
	al.deferExtraction("main", "req-restart", "I always order oat milk lattes", "cli")

	al2 := newTestAgentLoop(t, ws)
	if len(al2.readDeferredQueue()) != 1 {
		t.Fatal("the queue must survive a restart")
	}
	al2.semanticExtractor = newExtractorFor(p, al2)
	al2.FlushDeferred()
	if len(al2.readDeferredQueue()) != 0 {
		t.Fatal("the recovered queue must drain")
	}
}

// A failing extraction is retried a bounded number of times and then dropped,
// so one bad message cannot ask the model forever.
func TestDeferredExtractionRetriesThenDrops(t *testing.T) {
	al, _, _ := newProactiveLoop(t)
	al.semanticExtractor = newExtractorFor(&memProvider{fail: true}, al)
	al.deferExtraction("main", "req-fail", "I always order oat milk lattes", "cli")

	for i := 0; i < deferredMaxAttempts; i++ {
		al.FlushDeferred()
	}
	if got := len(al.readDeferredQueue()); got != 0 {
		t.Fatalf("a repeatedly failing record must be dropped, %d left", got)
	}
}

// The queue is bounded: a long outage cannot grow the file without limit.
func TestDeferredQueueIsBounded(t *testing.T) {
	al, _, _ := newProactiveLoop(t)
	al.semanticExtractor = newExtractorFor(&memProvider{}, al)
	for i := 0; i < deferredQueueMax+10; i++ {
		al.deferExtraction("main", "req-"+time.Duration(i).String(), "message", "cli")
	}
	if got := len(al.readDeferredQueue()); got > deferredQueueMax {
		t.Fatalf("queue grew to %d, cap is %d", got, deferredQueueMax)
	}
}

// Interactive work outranks background work.
func TestInteractivePriorityGate(t *testing.T) {
	al, _, _ := newProactiveLoop(t)
	if !al.BackgroundModelAllowed() {
		t.Fatal("idle runtime must allow background work")
	}
	al.beginInteractive()
	if al.BackgroundModelAllowed() || !al.interactiveBusy() {
		t.Fatal("an interactive turn must block background model work")
	}
	al.endInteractive()
	if !al.BackgroundModelAllowed() {
		t.Fatal("background work must resume once the turn ends")
	}
}
