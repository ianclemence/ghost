package tools

import (
	"context"
	"strings"
	"testing"
)

func pipeReg() *ToolRegistry {
	r := NewToolRegistry()
	r.Register(described{"search_files", "stub"})
	return r
}

type lister struct{ out string }

func (lister) Name() string        { return "list_dir" }
func (lister) Description() string { return "list" }
func (lister) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (l lister) Execute(context.Context, map[string]interface{}) *ToolResult {
	return NewToolResult(l.out)
}

type writer struct{}

func (writer) Name() string        { return "delete_file" }
func (writer) Description() string { return "deletes" }
func (writer) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (writer) Execute(context.Context, map[string]interface{}) *ToolResult {
	return NewToolResult("deleted")
}

func runPipe(t *testing.T, reg *ToolRegistry, steps []interface{}, ret string) *ToolResult {
	t.Helper()
	args := map[string]interface{}{"steps": steps}
	if ret != "" {
		args["return"] = ret
	}
	return NewPipelineTool(reg).Execute(context.Background(), args)
}

func TestPipelineKeepsIntermediateResultsOutOfTheAnswer(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(lister{out: "bank invoice march\nholiday photos\nbank statement april\nrecipe"})
	res := runPipe(t, reg, []interface{}{
		map[string]interface{}{"call": "list_dir", "args": map[string]interface{}{"path": "."}, "as": "files"},
		map[string]interface{}{"grep": "bank", "from": "files", "as": "hits"},
		map[string]interface{}{"count": true, "from": "hits", "as": "n"},
	}, "n")
	if res.IsError || res.ForLLM != "2" {
		t.Fatalf("expected only the count, got %+v", res)
	}
}

func TestPipelineRefusesAToolThatChangesThings(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(writer{})
	res := runPipe(t, reg, []interface{}{map[string]interface{}{"call": "delete_file", "args": map[string]interface{}{"path": "a"}}}, "")
	if !res.IsError || !strings.Contains(res.ForLLM, "not a read-only tool") {
		t.Fatalf("a write must be refused: %+v", res)
	}
}

func TestPipelineCannotCallItselfOrDelegate(t *testing.T) {
	for _, name := range []string{"pipeline", "subagent", "spawn", "batch_delegate"} {
		if isPipelineReadable(name, nil) {
			t.Errorf("%s must not be callable from a pipeline", name)
		}
	}
}

func TestPipelineRunsParallelCallsAndJoinsThem(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(lister{out: "a\nb"})
	res := runPipe(t, reg, []interface{}{
		map[string]interface{}{"parallel": []interface{}{
			map[string]interface{}{"call": "list_dir", "args": map[string]interface{}{"path": "x"}, "as": "one"},
			map[string]interface{}{"call": "list_dir", "args": map[string]interface{}{"path": "y"}, "as": "two"},
		}},
		map[string]interface{}{"join": []interface{}{"one", "two"}, "as": "all"},
		map[string]interface{}{"unique": true, "from": "all", "as": "u"},
	}, "u")
	if res.IsError || res.ForLLM != "a\nb" {
		t.Fatalf("unexpected: %+v", res)
	}
}

func TestPipelineReportsTheStepThatFailed(t *testing.T) {
	res := runPipe(t, NewToolRegistry(), []interface{}{map[string]interface{}{"grep": "x", "from": "nothing"}}, "")
	if !res.IsError || !strings.Contains(res.ForLLM, "step 1") {
		t.Fatalf("the failing step must be named: %+v", res)
	}
}

func TestPipelineBoundsStepsAndTheAnswer(t *testing.T) {
	many := make([]interface{}, pipelineMaxSteps+1)
	for i := range many {
		many[i] = map[string]interface{}{"count": true}
	}
	if res := runPipe(t, NewToolRegistry(), many, ""); !res.IsError {
		t.Fatal("too many steps must be refused")
	}
	reg := NewToolRegistry()
	reg.Register(lister{out: strings.Repeat("line\n", 5000)})
	res := runPipe(t, reg, []interface{}{map[string]interface{}{"call": "list_dir", "as": "x"}}, "x")
	if !strings.Contains(res.ForLLM, "[cut:") {
		t.Fatal("an oversized answer must be cut and say so")
	}
}

func TestPipelineResultIsTreatedAsOutsideContent(t *testing.T) {
	if !IsUntrustedSource("pipeline", NewPipelineTool(NewToolRegistry())) {
		t.Fatal("a pipeline may have read the web or an inbox; its result must be fenced")
	}
}
