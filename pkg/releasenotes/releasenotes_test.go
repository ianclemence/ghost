package releasenotes

import (
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/bench"
	"github.com/ianclemence/ghost/pkg/golden"
	"github.com/ianclemence/ghost/pkg/verify"
)

func testInput() Input {
	return Input{
		Version: "0.24.11", Commit: "abc1234", At: time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC),
		Model:         "deepseek/deepseek-flash",
		ChangelogBody: "Fixes.",
		VerifyCmd:     "ghost verify",
		BenchCmd:      "ghost eval benchmark",
		GoldenCmd:     "ghost eval golden --model=deepseek/deepseek-flash",
		Verify: &verify.Report{Overall: "PASS", Checks: []verify.Check{
			{Section: "Memory", Name: "memory write", Outcome: verify.Pass},
		}},
		Bench: &bench.Report{Overall: "PASS", Score: 100, ByDim: map[string]float64{"memory": 1},
			Metrics: []bench.Metric{{Dimension: "memory", Name: "write_success", Pass: true}}},
		Golden: &golden.Summary{SuiteVersion: 1, Model: "deepseek-flash", Provider: "deepseek",
			Total: 2, Passed: 2,
			ByCategory: map[golden.Category]golden.CatSummary{"memory": {Total: 2, Passed: 2}},
			Results: []golden.CaseResult{{ID: "mem-01", Category: "memory", Verdict: golden.VerdictPass}}},
	}
}

func TestRenderPassPage(t *testing.T) {
	out := Render(testInput())
	for _, want := range []string{
		"# Verification — Ghost 0.24.11", "## Verdict: PASS",
		"Overall PASS — 1 pass, 0 fail", "Ghost Core Score 100.0",
		"2/2 conversations pass", "## What's new", "## Limits",
		"## Reproduce", "ghost verify",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("page must contain %q\n%s", want, out)
		}
	}
}

func TestRenderFailPageNamesFailures(t *testing.T) {
	in := testInput()
	in.Verify.Overall = "FAIL"
	in.Verify.Checks = append(in.Verify.Checks, verify.Check{
		Section: "Security", Name: "no false success", Outcome: verify.Fail, Detail: "mapped", Hard: true,
	})
	in.Bench.Overall = "FAIL"
	in.Bench.HardFail = []string{"governance/zero_unauthorized"}
	in.Golden.Total, in.Golden.Passed, in.Golden.Failed = 2, 1, 1
	in.Golden.Results = append(in.Golden.Results, golden.CaseResult{
		ID: "cc-01", Category: "conversation", Verdict: golden.VerdictFail,
		Classification: golden.ModelBehavior,
		Assertions: []golden.AssertionResult{{Name: "no_false_success", Pass: false, Detail: "no evidence"}},
	})
	out := Render(in)
	for _, want := range []string{
		"## Verdict: FAIL", "Hard failures", "no false success",
		"governance/zero_unauthorized", "cc-01", "model_behavior",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("fail page must contain %q\n%s", want, out)
		}
	}
}

func TestRenderNilSuitesHonest(t *testing.T) {
	in := testInput()
	in.Verify, in.Bench, in.Golden = nil, nil, nil
	in.Model = ""
	out := Render(in)
	for _, want := range []string{
		"## Verdict: NO EVIDENCE", "Not run for this page",
		"Golden Conversation results are not attached",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("empty page must be honest, want %q\n%s", want, out)
		}
	}
}
