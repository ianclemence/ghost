// Package releasenotes assembles per-release verification pages from the
// machine-readable outputs Ghost already emits: verify.Report,
// bench.Report, golden.Summary, and the changelog entry body.
//
// It adds no new judgments. It counts, quotes, and names limits honestly:
// what ran, what passed, what failed, and what was not exercised. The
// output is docs/VERIFICATION-<version>.md, committed per release and
// linked from release notes — trust converted into marketing.
package releasenotes

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/bench"
	"github.com/ianclemence/ghost/pkg/golden"
	"github.com/ianclemence/ghost/pkg/verify"
)

// Input is everything one verification page renders.
type Input struct {
	Version       string
	Commit        string
	At            time.Time
	Model         string // model/provider the dynamic suites ran against, "" when none did
	Verify        *verify.Report
	Bench         *bench.Report
	Golden        *golden.Summary // nil when the golden suite was not run for this page
	ChangelogBody string
	// ExtraLimits names evaluation gaps the caller knows about (e.g.
	// "behavioral suite not run"). Standard limits derive from the inputs.
	ExtraLimits []string
	// VerifyCmd, BenchCmd, GoldenCmd record exactly how each suite ran.
	VerifyCmd string
	BenchCmd  string
	GoldenCmd string
}

// Render assembles the page.
func Render(in Input) string {
	var b strings.Builder
	at := in.At.UTC().Format("2006-01-02")
	b.WriteString(fmt.Sprintf("# Verification — Ghost %s\n\n", in.Version))
	b.WriteString(fmt.Sprintf("%s · commit `%s`", at, in.Commit))
	if in.Model != "" {
		b.WriteString(fmt.Sprintf(" · dynamic suites against `%s`", in.Model))
	}
	b.WriteString("\n\n")
	b.WriteString("What ran, what passed, what failed, and what was not exercised. ")
	b.WriteString("Every number below comes from Ghost's own checks; nothing here is asserted from prose.\n\n")

	overall, parts := verdict(in)
	b.WriteString(fmt.Sprintf("## Verdict: %s\n\n%s\n\n", overall, parts))

	if in.Verify != nil {
		renderVerify(&b, in.Verify, in.VerifyCmd)
	} else {
		b.WriteString("## Verification (`ghost verify`)\n\nNot run for this page.\n\n")
	}
	if in.Bench != nil {
		renderBench(&b, in.Bench, in.BenchCmd)
	} else {
		b.WriteString("## Benchmark (`ghost eval benchmark`)\n\nNot run for this page.\n\n")
	}
	if in.Golden != nil {
		renderGolden(&b, in.Golden, in.GoldenCmd)
	} else {
		b.WriteString("## Golden Conversation Suite (`ghost eval golden`)\n\nNot run for this page. Golden results below come from the most recent evaluated run only when attached with `--golden-json`.\n\n")
	}
	if strings.TrimSpace(in.ChangelogBody) != "" {
		b.WriteString("## What's new\n\n")
		b.WriteString(strings.TrimSpace(in.ChangelogBody) + "\n\n")
	}
	renderLimits(&b, in)
	b.WriteString("## Reproduce\n\n")
	b.WriteString("```\n")
	for _, c := range []string{in.VerifyCmd, in.BenchCmd, in.GoldenCmd} {
		if strings.TrimSpace(c) != "" {
			b.WriteString(c + "\n")
		}
	}
	b.WriteString("```\n")
	return b.String()
}

func verdict(in Input) (string, string) {
	var fails []string
	var ran []string
	if in.Verify != nil {
		ran = append(ran, fmt.Sprintf("verify %s", in.Verify.Overall))
		if in.Verify.Overall != "PASS" {
			fails = append(fails, "verify")
		}
	}
	if in.Bench != nil {
		ran = append(ran, fmt.Sprintf("benchmark %s (%.1f)", in.Bench.Overall, in.Bench.Score))
		if in.Bench.Overall != "PASS" {
			fails = append(fails, "benchmark")
		}
	}
	if in.Golden != nil {
		g := in.Golden
		ran = append(ran, fmt.Sprintf("golden %d/%d pass", g.Passed, g.Total))
		if g.Failed > 0 {
			fails = append(fails, "golden")
		}
	}
	if len(ran) == 0 {
		return "NO EVIDENCE", "No suite ran for this page."
	}
	if len(fails) == 0 {
		return "PASS", "All suites that ran passed: "+strings.Join(ran, "; ")+"."
	}
	return "FAIL", "Ran: "+strings.Join(ran, "; ")+". Failing: "+strings.Join(fails, ", ")+"."
}

func renderVerify(b *strings.Builder, r *verify.Report, cmd string) {
	pass, fail, skip := 0, 0, 0
	var hard []verify.Check
	for _, c := range r.Checks {
		switch c.Outcome {
		case verify.Pass:
			pass++
		case verify.Fail:
			fail++
			if c.Hard {
				hard = append(hard, c)
			}
		default:
			skip++
		}
	}
	b.WriteString(fmt.Sprintf("## Verification (`ghost verify`)\n\nOverall %s — %d pass, %d fail, %d skip/not-run.\n\n", r.Overall, pass, fail, skip))
	if len(hard) > 0 {
		b.WriteString("Hard failures (security invariants):\n\n")
		for _, c := range hard {
			b.WriteString(fmt.Sprintf("- **%s / %s** — %s\n", c.Section, c.Name, c.Detail))
		}
		b.WriteString("\n")
	}
	for _, c := range r.Checks {
		if c.Outcome == verify.Fail && !c.Hard {
			b.WriteString(fmt.Sprintf("- %s / %s — %s\n", c.Section, c.Name, c.Detail))
		}
	}
	if fail > 0 {
		b.WriteString("\n")
	}
	if strings.TrimSpace(cmd) != "" {
		b.WriteString(fmt.Sprintf("Ran as: `%s`\n\n", cmd))
	}
}

func renderBench(b *strings.Builder, r *bench.Report, cmd string) {
	b.WriteString(fmt.Sprintf("## Benchmark (`ghost eval benchmark`)\n\nGhost Core Score %.1f — overall %s.\n\n", r.Score, r.Overall))
	dims := make([]string, 0, len(r.ByDim))
	for d := range r.ByDim {
		dims = append(dims, d)
	}
	sort.Strings(dims)
	b.WriteString("| Dimension | Pass rate |\n|---|---|\n")
	for _, d := range dims {
		b.WriteString(fmt.Sprintf("| %s | %.0f%% |\n", d, r.ByDim[d]*100))
	}
	b.WriteString("\n")
	if len(r.HardFail) > 0 {
		b.WriteString("Hard failures: " + strings.Join(r.HardFail, ", ") + "\n\n")
	}
	for _, m := range r.Metrics {
		if !m.Pass {
			b.WriteString(fmt.Sprintf("- %s/%s failed (%s)\n", m.Dimension, m.Name, m.Detail))
		}
	}
	if strings.TrimSpace(cmd) != "" {
		b.WriteString(fmt.Sprintf("\nRan as: `%s`\n\n", cmd))
	}
}

func renderGolden(b *strings.Builder, s *golden.Summary, cmd string) {
	b.WriteString(fmt.Sprintf("## Golden Conversation Suite (`ghost eval golden`)\n\n%d/%d conversations pass", s.Passed, s.Total))
	if s.Skipped > 0 {
		b.WriteString(fmt.Sprintf(", %d skipped", s.Skipped))
	}
	if s.HardFails > 0 {
		b.WriteString(fmt.Sprintf(", %d with hard failures", s.HardFails))
	}
	b.WriteString(fmt.Sprintf(" (suite v%d, model `%s/%s`).\n\n", s.SuiteVersion, s.Provider, s.Model))
	cats := make([]string, 0, len(s.ByCategory))
	for c := range s.ByCategory {
		cats = append(cats, string(c))
	}
	sort.Strings(cats)
	b.WriteString("| Category | Pass | Fail | Skipped |\n|---|---|---|---|\n")
	for _, c := range cats {
		cs := s.ByCategory[golden.Category(c)]
		b.WriteString(fmt.Sprintf("| %s | %d | %d | %d |\n", c, cs.Passed, cs.Failed, cs.Skipped))
	}
	b.WriteString("\n")
	for _, r := range s.Results {
		if r.Verdict == golden.VerdictFail {
			b.WriteString(fmt.Sprintf("- **%s** (%s): %s", r.ID, r.Category, r.Classification))
			for _, a := range r.Assertions {
				if !a.Pass {
					b.WriteString(fmt.Sprintf("; %s: %s", a.Name, a.Detail))
				}
			}
			b.WriteString("\n")
		}
	}
	if strings.TrimSpace(cmd) != "" {
		b.WriteString(fmt.Sprintf("\nRan as: `%s`\n\n", cmd))
	}
}

func renderLimits(b *strings.Builder, in Input) {
	b.WriteString("## Limits — what this page does not establish\n\n")
	limits := []string{}
	if in.Golden == nil {
		limits = append(limits, "Golden Conversation results are not attached; end-to-end model behavior is covered by verify/benchmark only.")
	}
	live := false
	if in.Verify != nil {
		for _, c := range in.Verify.Checks {
			if c.Outcome == verify.NotRun {
				live = true
			}
		}
	}
	if !live {
		limits = append(limits, "Live-vendor checks did not run (no network credentials in the evaluation environment).")
	}
	limits = append(limits, "Qwen-class local models are supported but intentionally not run on the reference device; selection reports supported/not-run.")
	limits = append(limits, "Results describe the cited commit and model only; a different provider, model, or workspace may behave differently.")
	limits = append(limits, in.ExtraLimits...)
	for _, l := range limits {
		if strings.TrimSpace(l) != "" {
			b.WriteString("- " + l + "\n")
		}
	}
	b.WriteString("\n")
}
