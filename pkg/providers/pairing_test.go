package providers

import "testing"

func call(id string) ToolCall {
	return ToolCall{ID: id, Type: "function", Function: &FunctionCall{Name: "t"}}
}

func tool(id string) Message { return Message{Role: "tool", ToolCallID: id, Content: "r-" + id} }

func TestRepairAnswersACallThatNeverRan(t *testing.T) {
	in := []Message{
		{Role: "user", Content: "delete it"},
		{Role: "assistant", ToolCalls: []ToolCall{call("a"), call("b")}},
		tool("a"),
		{Role: "user", Content: "is it gone?"},
	}
	out := RepairToolPairing(in)
	if len(out) != 5 || out[3].Role != "tool" || out[3].ToolCallID != "b" || out[3].Content == "" {
		t.Fatalf("the unanswered call must get a stand-in right after its siblings: %+v", out)
	}
	if out[2].Content != "r-a" {
		t.Fatal("the real result must be kept")
	}
}

func TestRepairDropsToolMessagesWithoutACall(t *testing.T) {
	out := RepairToolPairing([]Message{tool("x"), {Role: "user", Content: "hi"}, tool("y")})
	if len(out) != 1 || out[0].Role != "user" {
		t.Fatalf("orphaned tool messages must go: %+v", out)
	}
}

func TestRepairLeavesAValidConversationAlone(t *testing.T) {
	in := []Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []ToolCall{call("a")}},
		tool("a"),
		{Role: "assistant", Content: "done"},
	}
	out := RepairToolPairing(in)
	if len(out) != len(in) {
		t.Fatalf("a valid conversation changed: %+v", out)
	}
}

func TestRepairIgnoresDuplicateAndForeignResults(t *testing.T) {
	out := RepairToolPairing([]Message{
		{Role: "assistant", ToolCalls: []ToolCall{call("a")}},
		tool("a"), tool("a"), tool("zzz"),
	})
	if len(out) != 2 {
		t.Fatalf("one answer per call, nothing foreign: %+v", out)
	}
}

func TestUnansweredToolCallsNamesOnlyTheLastTurn(t *testing.T) {
	got := UnansweredToolCalls([]Message{
		{Role: "assistant", ToolCalls: []ToolCall{call("old")}},
		{Role: "assistant", ToolCalls: []ToolCall{call("a"), call("b")}},
		tool("a"),
	})
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("got %v", got)
	}
}
