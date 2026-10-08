package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/tools"
)

func TestToolDetailIsOneSafeLine(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args map[string]interface{}
		want string
	}{
		{"search keeps the query", "web_search", map[string]interface{}{"query": "nairobi bar culture"}, "nairobi bar culture"},
		{"fetch keeps only the host", "web_fetch", map[string]interface{}{"url": "https://www.example.com/a/b?token=abc"}, "example.com"},
		{"navigate keeps only the host", "browser_navigate", map[string]interface{}{"url": "news.ycombinator.com/item?id=1"}, "news.ycombinator.com"},
		{"exec is one line", "exec", map[string]interface{}{"command": "php -v\nphp -m"}, "php -v"},
		{"files show the name, not the path", "read_file", map[string]interface{}{"path": "/home/x/secret/notes.md"}, "notes.md"},
		{"others say nothing", "remember", map[string]interface{}{"text": "private"}, ""},
	}
	for _, c := range cases {
		if got := ToolDetail(c.tool, c.args); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestSafeLineMasksCredentialsAndCuts(t *testing.T) {
	got := SafeLine("curl -H 'Authorization: Bearer abc123def456' https://x.y", 200)
	if strings.Contains(got, "abc123def456") {
		t.Fatalf("credential leaked: %q", got)
	}
	got = SafeLine("export OPENAI_API_KEY=sk-abcdefghijklmnop1234", 200)
	if strings.Contains(got, "abcdefghijklmnop1234") {
		t.Fatalf("key leaked: %q", got)
	}
	got = SafeLine(strings.Repeat("word ", 60), 40)
	if n := len([]rune(got)); n > 40 {
		t.Fatalf("not cut: %d runes", n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("cut without ellipsis: %q", got)
	}
}

func TestTurnObserverReceivesEventsOnlyWhenAttached(t *testing.T) {
	emitTurnEvent(context.Background(), map[string]interface{}{"type": "x"}) // no watcher: no panic
	var seen []string
	ctx := WithTurnObserver(context.Background(), func(ev map[string]interface{}) {
		seen = append(seen, ev["type"].(string))
	})
	emitTurnEvent(ctx, toolStartEvent("c1", "web_search", map[string]interface{}{"query": "q"}))
	emitTurnEvent(ctx, toolEndEvent("c1", "web_search", true, 120, ""))
	if len(seen) != 2 || seen[0] != "tool_start" || seen[1] != "tool_result" {
		t.Fatalf("events: %v", seen)
	}
	ev := toolEndEvent("c2", "exec", false, 5, "exit status 1\nmore")
	if ev["note"] != "exit status 1" {
		t.Fatalf("note: %v", ev["note"])
	}
}

func TestSteerPickedEventListsOnlyOwnerMessages(t *testing.T) {
	ev := steerPickedEvent([]SteeringMessage{
		{Content: "use blue"},
		{Content: "[SYSTEM] stop", IsInterrupt: true},
		{Content: "and bold"},
	})
	got := ev["contents"].([]string)
	if len(got) != 2 || got[0] != "use blue" || got[1] != "and bold" {
		t.Fatalf("contents: %v", got)
	}
}

func TestAFailedCommandSaysWhatTheCommandSaid(t *testing.T) {
	cases := []struct{ name, out, want string }{
		{"stderr line, not the marker", "\nSTDERR:\nls: cannot access '/x': No such file or directory\nExit code: exit status 2", "ls: cannot access '/x': No such file or directory"},
		{"stdout first when there is some", "partial output\nSTDERR:\nboom\nExit code: exit status 1", "partial output"},
		{"nothing said: the exit status", "(no output)\nExit code: exit status 3", "exit status 3"},
		{"plain error text", "tool \"x\" not found", "tool \"x\" not found"},
	}
	for _, c := range cases {
		got := toolEndNote(&tools.ToolResult{IsError: true, ForLLM: c.out})
		if got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	if toolEndNote(&tools.ToolResult{ForLLM: "all good"}) != "" {
		t.Error("a call that worked has no note")
	}
	if toolEndNote(&tools.ToolResult{TimedOut: true, IsError: true}) != "Timed out" {
		t.Error("a timeout says so")
	}
}
