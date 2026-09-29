package agent

import (
	"context"
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
)

// The owner is told where a turn ran from execution: the provider behind
// the candidate that actually served, with locality decided by provider.
func TestServedByNamesProviderAndLocality(t *testing.T) {
	al := &AgentLoop{cfg: &config.Config{}}
	al.cfg.Agents.Defaults.Provider = "ollama"
	if s := al.servedBy("qwen3:0.6b", "qwen3:0.6b"); s.Provider != "ollama" || !s.Local {
		t.Fatalf("bare local model: %+v", s)
	}
	if s := al.servedBy("deepseek:deepseek-flash", "deepseek-flash"); s.Provider != "deepseek" || s.Local || s.Model != "deepseek-flash" {
		t.Fatalf("cloud fallback: %+v", s)
	}
}

func TestAggregateServedByIsCloudIfAnyCallWas(t *testing.T) {
	agg, ok := AggregateServedBy([]ServedBy{{Provider: "ollama", Model: "q", Local: true}, {Provider: "deepseek", Model: "d", Local: false}, {Provider: "ollama", Model: "q", Local: true}})
	if !ok || agg.Local || agg.Model != "q" {
		t.Fatalf("one cloud call makes the turn cloud: %+v", agg)
	}
	if _, ok := AggregateServedBy(nil); ok {
		t.Fatal("no records, no claim")
	}
}

// A real turn reports what served it through the sink.
func TestTurnReportsServedBy(t *testing.T) {
	prov := &droppingStreamProvider{calls: 1} // answers first time
	al := newTestAgentLoopWithProvider(t, t.TempDir(), prov)
	var got []ServedBy
	ctx := WithServedBySink(context.Background(), func(s ServedBy) { got = append(got, s) })
	if _, err := al.ProcessDirectWithChannel(ctx, "hello", "served", "web", "chat", nil, func(string) {}, nil); err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[len(got)-1].Model == "" {
		t.Fatalf("turn served without a record: %+v", got)
	}
}
