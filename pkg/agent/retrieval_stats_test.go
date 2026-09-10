package agent

import "testing"

func TestLatencyScale(t *testing.T) {
	var r retrievalRecorder
	if got := r.latencyScale(); got != 1 {
		t.Fatalf("unmeasured latency must not be penalized, got %v", got)
	}
	r.observe("rag", 100)
	if got := r.latencyScale(); got != 1 {
		t.Fatalf("fast retrieval scale = %v", got)
	}
	var rMed retrievalRecorder
	rMed.observe("rag", 500)
	if got := rMed.latencyScale(); got != 0.5 {
		t.Fatalf("medium retrieval scale = %v", got)
	}
	var rSlow retrievalRecorder
	rSlow.observe("rag", 2000)
	if got := rSlow.latencyScale(); got != 0.25 {
		t.Fatalf("slow retrieval scale = %v", got)
	}
	// The worst observed path wins.
	var r2 retrievalRecorder
	r2.observe("rag", 100)
	r2.observe("memo", 1500)
	if got := r2.latencyScale(); got != 0.25 {
		t.Fatalf("worst path must dominate, got %v", got)
	}
}

func TestLocalModelFits(t *testing.T) {
	// Cloud models always fit (no local RAM cost).
	if !localModelFits("deepseek/deepseek-flash", 100) {
		t.Fatal("cloud model must fit")
	}
	// Unknown local size is allowed (do not block what cannot be measured).
	if !localModelFits("ollama/mystery", 100) {
		t.Fatal("unknown-size local model must fit")
	}
	// A measured large local model does not fit in little memory.
	if localModelFits("ollama/llama-70b", 4000) {
		t.Fatal("70b must not fit in 4GB")
	}
	// ...but fits with enough headroom.
	if !localModelFits("ollama/llama-70b", 48000) {
		t.Fatal("70b must fit in 48GB")
	}
}
