package tools

import (
	"context"
	"strings"
	"testing"
)

func rows() []SelfCheckRow {
	return []SelfCheckRow{
		{Name: "database", Label: "Database", Status: "ok", Message: "integrity ok"},
		{Name: "browser", Label: "Browser", Status: "warning", Message: "no browser found"},
		{Name: "provider", Label: "AI model", Status: "ok"},
	}
}

func TestSelfCheckVerdict(t *testing.T) {
	if SelfCheckVerdict(rows()[:1]) != "healthy" || SelfCheckVerdict(rows()) != "needs attention" {
		t.Fatal("ok and warning verdicts")
	}
	if SelfCheckVerdict(append(rows(), SelfCheckRow{Status: "error"})) != "problems" {
		t.Fatal("any error is a problem")
	}
}

func TestSelfCheckToolReportsEverythingItFound(t *testing.T) {
	SetRunningVersion("v9.9.9")
	res := NewSelfCheckTool(func(context.Context) []SelfCheckRow { return rows() }).Execute(context.Background(), nil)
	for _, want := range []string{"v9.9.9", "needs attention", "Database [ok]", "no browser found", "AI model [ok]"} {
		if !strings.Contains(res.ForLLM, want) {
			t.Errorf("missing %q in:\n%s", want, res.ForLLM)
		}
	}
	if bad := NewSelfCheckTool(nil).Execute(context.Background(), nil); !bad.IsError {
		t.Fatal("an unwired check must say so, not report health")
	}
}

func TestProblemsOnlyKeepsTheNoiseOut(t *testing.T) {
	out := FormatSelfCheck("v1", rows(), true)
	if strings.Contains(out, "Database") || !strings.Contains(out, "Browser [warning]") || !strings.Contains(out, "2 other check(s) are fine") {
		t.Fatalf("got:\n%s", out)
	}
}
