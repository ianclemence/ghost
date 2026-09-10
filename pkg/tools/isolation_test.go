package tools

import (
	"context"
	"strings"
	"testing"
)

func TestWrapArgvModes(t *testing.T) {
	base := []string{"sh", "-c", "echo hi"}

	t.Setenv("GHOST_EXEC_ISOLATION", "off")
	if got, wrapped, err := WrapArgv(base, "/tmp", "/tmp", true); err != nil || wrapped || strings.Join(got, " ") != strings.Join(base, " ") {
		t.Fatalf("off mode must pass through: %v %v %v", got, wrapped, err)
	}

	t.Setenv("GHOST_EXEC_ISOLATION", "auto")
	got, wrapped, err := WrapArgv(base, "/tmp", "/tmp", true)
	if err != nil {
		t.Fatalf("auto: %v", err)
	}
	if bwrapAvailable() {
		if !wrapped || got[0] != "bwrap" {
			t.Fatalf("auto with bwrap must wrap: %v wrapped=%v", got, wrapped)
		}
	} else if wrapped {
		t.Fatal("auto without bwrap must not wrap")
	}
}

func TestWrapArgvRequireFailsWithoutBwrap(t *testing.T) {
	if bwrapAvailable() {
		t.Skip("bwrap is available; require path would succeed")
	}
	t.Setenv("GHOST_EXEC_ISOLATION", "require")
	if _, _, err := WrapArgv([]string{"sh", "-c", "true"}, "/tmp", "/tmp", true); err == nil {
		t.Fatal("require mode must fail when isolation is unavailable")
	}
}

// The exec tool must still run ordinary commands and write inside the
// workspace under isolation.
func TestExecToolRunsUnderIsolation(t *testing.T) {
	ws := t.TempDir()
	tool := NewExecTool(ws, false)
	res := tool.Execute(context.Background(), map[string]interface{}{
		"command": "echo isolated-ok && echo data > out.txt && cat out.txt",
	})
	if res.IsError {
		t.Fatalf("exec failed: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "isolated-ok") || !strings.Contains(res.ForLLM, "data") {
		t.Fatalf("unexpected output: %s", res.ForLLM)
	}
}
