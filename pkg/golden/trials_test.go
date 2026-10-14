package golden

import (
	"strings"
	"testing"
)

func run(verdicts map[string]Verdict) Summary {
	s := Summary{Model: "m", Provider: "p"}
	for id, v := range verdicts {
		s.Results = append(s.Results, CaseResult{ID: id, Title: id, Category: CatMemory, Verdict: v})
	}
	return s
}

func TestAggregateTrialsSeparatesCapableFromReliable(t *testing.T) {
	rep := AggregateTrials([]Summary{
		run(map[string]Verdict{"a": VerdictPass, "b": VerdictPass, "c": VerdictFail}),
		run(map[string]Verdict{"a": VerdictPass, "b": VerdictFail, "c": VerdictFail}),
		run(map[string]Verdict{"a": VerdictPass, "b": VerdictPass, "c": VerdictFail}),
	})
	if rep.K != 3 || len(rep.Cases) != 3 {
		t.Fatalf("unexpected shape: %+v", rep)
	}
	if got := rep.PassAtK(); got < 0.66 || got > 0.67 {
		t.Fatalf("pass@k = %v, want 2/3", got)
	}
	if got := rep.PassHatK(); got < 0.33 || got > 0.34 {
		t.Fatalf("pass^k = %v, want 1/3", got)
	}
	flaky := rep.Flaky()
	if len(flaky) != 1 || flaky[0].ID != "b" {
		t.Fatalf("flaky = %+v, want only b", flaky)
	}
}

func TestAggregateTrialsIgnoresSkippedAndCountsHardFailAsFail(t *testing.T) {
	hard := run(map[string]Verdict{"a": VerdictPass})
	hard.Results[0].Assertions = []AssertionResult{{Name: "no secret leak", Pass: false, Hard: true}}
	rep := AggregateTrials([]Summary{hard, run(map[string]Verdict{"a": VerdictPass, "s": VerdictSkip})})
	if len(rep.Cases) != 1 {
		t.Fatalf("skipped case counted: %+v", rep.Cases)
	}
	if rep.Cases[0].Passes != 1 || rep.Cases[0].Trials != 2 {
		t.Fatalf("a hard-failed pass must not count: %+v", rep.Cases[0])
	}
}

func TestRenderTrialsNamesFlakyCases(t *testing.T) {
	rep := AggregateTrials([]Summary{run(map[string]Verdict{"x": VerdictPass}), run(map[string]Verdict{"x": VerdictFail})})
	out := RenderTrials(rep)
	if !strings.Contains(out, "pass^2") || !strings.Contains(out, "x") {
		t.Fatalf("report missing the reliability line or the flaky case:\n%s", out)
	}
}

func TestHoldoutIsStableAndPartitionsTheSuite(t *testing.T) {
	var suite []Conversation
	for i := 0; i < 200; i++ {
		suite = append(suite, Conversation{ID: "case-" + itoa(i)})
	}
	dev, hold := FilterSplit(suite, SplitDev), FilterSplit(suite, SplitHoldout)
	if len(dev)+len(hold) != len(suite) {
		t.Fatal("dev and holdout must cover the suite exactly")
	}
	if len(hold) < 20 || len(hold) > 60 {
		t.Fatalf("holdout is %d of 200, expected about a fifth", len(hold))
	}
	again := FilterSplit(suite, SplitHoldout)
	for i := range hold {
		if hold[i].ID != again[i].ID {
			t.Fatal("holdout changed between calls")
		}
	}
}

func entry(model string, passed, total int, cats map[Category]CatSummary) HistoryEntry {
	return HistoryEntry{Model: model, Provider: "p", At: "t", Summary: Summary{Model: model, Provider: "p", Passed: passed, Total: total, ByCategory: cats}}
}

func TestScorecardTracksChangeAndSaturation(t *testing.T) {
	full := map[Category]CatSummary{CatMemory: {Total: 4, Passed: 4}, CatPermission: {Total: 4, Passed: 3}}
	rows := Scorecard([]HistoryEntry{
		entry("a", 8, 10, full), entry("a", 9, 10, full), entry("a", 10, 10, full), entry("b", 5, 10, full),
	})
	if len(rows) != 2 {
		t.Fatalf("want a row per model, got %d", len(rows))
	}
	a := rows[0]
	if a.Model != "a" || a.Latest != 1.0 || a.Previous != 0.9 {
		t.Fatalf("bad row: %+v", a)
	}
	if len(a.Saturated) != 1 || a.Saturated[0] != CatMemory {
		t.Fatalf("only memory passed everything three runs running: %v", a.Saturated)
	}
	if len(rows[1].Saturated) != 0 {
		t.Fatal("a model with fewer than three runs cannot be saturated")
	}
}

func TestCaptureEnvReportsTheMachine(t *testing.T) {
	e := CaptureEnv(t.TempDir())
	if e.Arch == "" || e.CPUs < 1 {
		t.Fatalf("env missing basics: %+v", e)
	}
}
