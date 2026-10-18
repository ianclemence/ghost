package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/tools"
)

func TestUpdateOutcomeSaysWhatHappened(t *testing.T) {
	ok := []tools.SelfCheckRow{{Name: "database", Status: "ok"}}
	bad := []tools.SelfCheckRow{{Name: "database", Status: "ok"}, {Name: "browser", Label: "Browser", Status: "error", Message: "crashed"}}

	if got := updateOutcomeText("v1", "v2", ok); !strings.Contains(got, "now on v2 (it was v1)") || !strings.Contains(got, "everything is working") {
		t.Errorf("healthy update: %q", got)
	}
	if got := updateOutcomeText("v1", "v1", ok); !strings.Contains(got, "didn't take") {
		t.Errorf("rolled back: %q", got)
	}
	got := updateOutcomeText("v1", "v2", bad)
	if !strings.Contains(got, "problems") || !strings.Contains(got, "Browser [error]: crashed") || strings.Contains(got, "everything is working") {
		t.Errorf("unhealthy update must not claim health: %q", got)
	}
	if got := updateOutcomeText("v1", "v2", nil); !strings.Contains(got, "can't say how I'm doing") {
		t.Errorf("no check: %q", got)
	}
}

func TestDevConfigNeverPointsAtTheInstalledWorkspace(t *testing.T) {
	installed := []byte(`{"agents":{"defaults":{"workspace":"/var/lib/ghost/workspace","model":"m"}},"keep":1}`)
	out, err := devConfigFor(installed, "/tmp/dev/workspace")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	d := got["agents"].(map[string]interface{})["defaults"].(map[string]interface{})
	if d["workspace"] != "/tmp/dev/workspace" || d["model"] != "m" || got["keep"].(float64) != 1 {
		t.Fatalf("got %v", got)
	}
	if _, err := devConfigFor([]byte("{not json"), "/x"); err == nil {
		t.Fatal("a broken config must be reported, not copied")
	}
}
