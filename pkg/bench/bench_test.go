package bench

import (
	"strings"
	"testing"
	"time"
)

func TestBenchmarkRuns(t *testing.T) {
	rep := Run("", 120*time.Second)
	if len(rep.Metrics) < 20 {
		t.Fatalf("benchmark too thin: %d metrics", len(rep.Metrics))
	}
	if rep.Overall != "PASS" {
		var fails []string
		for _, m := range rep.Metrics {
			if !m.Pass {
				fails = append(fails, m.Dimension+"/"+m.Name+": "+m.Detail)
			}
		}
		t.Fatalf("benchmark failed (score %.1f):\n%s", rep.Score, strings.Join(fails, "\n"))
	}
	if rep.Score < 90 {
		t.Fatalf("core score too low: %.1f", rep.Score)
	}
	t.Logf("\n%s", Render(rep))
}

func TestHistoryRoundtrip(t *testing.T) {
	ws := t.TempDir()
	rep := Run("", 120*time.Second)
	hist, err := SaveHistory(ws, rep, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 {
		t.Fatal("history must persist")
	}
	rep2 := Run("", 120*time.Second)
	hist, err = SaveHistory(ws, rep2, 5)
	if err != nil || len(hist) != 2 {
		t.Fatal("history must accumulate")
	}
	cmp := Compare(hist[0], hist[1])
	if !strings.Contains(cmp, "score") {
		t.Fatal("compare must summarize score")
	}
}
