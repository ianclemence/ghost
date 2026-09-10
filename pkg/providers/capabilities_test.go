package providers

import "testing"

func TestDescribeCloudDeepSeek(t *testing.T) {
	c := Describe("deepseek/deepseek-flash")
	if c.Provider != "deepseek" || c.ID != "deepseek-flash" {
		t.Fatalf("split wrong: %+v", c)
	}
	if c.Local {
		t.Fatal("deepseek is cloud")
	}
	if !c.Vision || !c.Reasoning || !c.ToolCalling {
		t.Fatalf("deepseek-flash capabilities wrong: %+v", c)
	}
	if c.MaxContext != 1_000_000 {
		t.Fatalf("max context = %d", c.MaxContext)
	}
}

func TestDescribeLocalRAMEstimate(t *testing.T) {
	c := Describe("ollama/qwen3:0.6b")
	if !c.Local {
		t.Fatal("ollama is local")
	}
	if c.EstimatedRAMMB == 0 {
		t.Fatal("local model must carry a RAM estimate for hardware-aware routing")
	}
}

func TestDescribeUnknownIsConservative(t *testing.T) {
	c := Describe("mystery/model-x")
	if c.ToolCalling != true {
		t.Fatal("unknown models default to tool calling")
	}
	if c.Vision || c.Reasoning {
		t.Fatal("unknown models must not claim vision/reasoning")
	}
	if c.MaxContext != 8192 {
		t.Fatalf("unknown max context = %d", c.MaxContext)
	}
}
