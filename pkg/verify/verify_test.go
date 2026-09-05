package verify

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVerifySuitePasses(t *testing.T) {
	rep := Run(Options{Timeout: 120000000000})
	fails := []string{}
	for _, c := range rep.Checks {
		if c.Outcome == Fail {
			fails = append(fails, c.Section+"/"+c.Name+": "+c.Detail)
		}
	}
	if rep.Overall != "PASS" {
		t.Fatalf("verify failed:\n%s", strings.Join(fails, "\n"))
	}
	if len(rep.Checks) < 30 {
		t.Fatalf("suite too small: %d checks", len(rep.Checks))
	}
}

func TestRenderHuman(t *testing.T) {
	rep := Run(Options{Timeout: 120000000000})
	out := Render(rep)
	if !strings.Contains(out, "GHOST VERIFICATION") || !strings.Contains(out, "OVERALL:") {
		t.Fatal("report must be human-readable with overall verdict")
	}
	raw, err := json.Marshal(rep)
	if err != nil || !strings.Contains(string(raw), `"overall"`) {
		t.Fatal("report must be machine-readable")
	}
}
