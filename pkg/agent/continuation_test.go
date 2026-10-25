package agent

import (
	"strings"
	"testing"
)

// The live failure: Ghost asked to delete projects/scout with recursive true.
// The owner approved. The tool ran with only the path, "needs recursive", and
// nothing was deleted: the pause record kept only short strings.
func TestAnApprovedActionRunsWithTheParametersItWasAskedWith(t *testing.T) {
	g := openTestGovernance(t)
	asked := map[string]interface{}{
		"path": "projects/scout", "recursive": true, "timeout_seconds": float64(900),
		"paths": []interface{}{"a", "b"}, "options": map[string]interface{}{"dry_run": true},
		"content": strings.Repeat("x", 3000),
	}
	res := g.AuthorizeTool("r1", "s1", "file.delete", "delete_file", asked)
	if res.Allowed || res.PendingID == "" {
		t.Fatalf("a consequential action must wait: %+v", res)
	}
	out := g.CheckApprovalReply("s1", "allow once")
	if !out.Resumed || out.Tool != "delete_file" {
		t.Fatalf("approval must resume the call: %+v", out)
	}
	if out.Args["recursive"] != true {
		t.Fatalf("recursive was lost: %#v", out.Args["recursive"])
	}
	if out.Args["timeout_seconds"] != float64(900) {
		t.Fatalf("a number lost its type or value: %#v", out.Args["timeout_seconds"])
	}
	if list, ok := out.Args["paths"].([]interface{}); !ok || len(list) != 2 {
		t.Fatalf("a list was lost: %#v", out.Args["paths"])
	}
	if opts, ok := out.Args["options"].(map[string]interface{}); !ok || opts["dry_run"] != true {
		t.Fatalf("a nested dry_run was lost: %#v", out.Args["options"])
	}
	if s, _ := out.Args["content"].(string); len(s) != 3000 {
		t.Fatalf("long content was truncated or dropped: %d", len(s))
	}
}

// A secret is never stored, and an approval that would have to run without one
// is refused, not run as a different action.
func TestAnApprovalThatWouldLoseAnArgumentIsRefused(t *testing.T) {
	g := openTestGovernance(t)
	g.AuthorizeTool("r1", "s1", "email.send", "exec", map[string]interface{}{"to": "a@b.c", "api_key": "sk-123456", "keyword": "invoice"})
	pending, ok := g.Broker.PendingForSession("s1")
	if !ok {
		t.Fatal("expected a pending request")
	}
	for k, v := range pending.Continuation {
		if strings.Contains(strings.ToLower(k), "api_key") || strings.Contains(v, "sk-123456") {
			t.Fatalf("a secret reached the stored record: %q=%q", k, v)
		}
	}
	if pending.Continuation["keyword"] != "invoice" {
		t.Fatalf("a search keyword must survive: %#v", pending.Continuation)
	}
	out := g.CheckApprovalReply("s1", "allow once")
	if out.Resumed {
		t.Fatalf("an action missing a parameter must not run: %+v", out)
	}
	if !strings.Contains(out.Message, "api_key") || !strings.Contains(out.Message, "Ask me again") {
		t.Fatalf("the owner must be told why and what to do: %q", out.Message)
	}
}

func TestContinuationRoundTripAndSecretNames(t *testing.T) {
	args := map[string]interface{}{"n": float64(3), "ok": false, "s": "plain", "tags": []interface{}{"x"}}
	back, dropped := argsFromContinuation(continuationOf(args))
	if len(dropped) != 0 || back["n"] != float64(3) || back["ok"] != false || back["s"] != "plain" {
		t.Fatalf("round trip changed the arguments: %#v dropped=%v", back, dropped)
	}
	if got, _ := continuationOf(map[string]interface{}{"password": "x"})[contDroppedKey]; got != "password" {
		t.Fatalf("a dropped secret must be named, not hidden: %q", got)
	}
}
