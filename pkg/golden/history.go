package golden

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// HistoryEntry is one persisted golden run.
type HistoryEntry struct {
	At       string  `json:"at"`
	Commit   string  `json:"commit,omitempty"`
	Model    string  `json:"model"`
	Provider string  `json:"provider"`
	SuiteVer int     `json:"suite_version"`
	Summary  Summary `json:"summary"`
}

// LoadHistory reads prior golden runs from the workspace state dir.
func LoadHistory(workspace string) ([]HistoryEntry, error) {
	path := filepath.Join(workspace, "state", "golden-history.json")
	var entries []HistoryEntry
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return entries, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// SaveHistory appends a run, keeping at most keep entries (local file only;
// no new database).
func SaveHistory(workspace string, s Summary, keep int) ([]HistoryEntry, error) {
	entries, err := LoadHistory(workspace)
	if err != nil {
		return nil, err
	}
	e := HistoryEntry{At: s.At, Commit: s.Commit, Model: s.Model, Provider: s.Provider, SuiteVer: s.SuiteVersion, Summary: s}
	if e.At == "" {
		e.At = time.Now().Format(time.RFC3339)
	}
	entries = append(entries, e)
	if keep > 0 && len(entries) > keep {
		entries = entries[len(entries)-keep:]
	}
	dir := filepath.Join(workspace, "state")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "golden-history.json")
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return nil, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return nil, err
	}
	return entries, os.Rename(tmp, path)
}

// RenderSummary prints the human-readable golden report.
func RenderSummary(s Summary) string {
	var b strings.Builder
	b.WriteString("\nGHOST GOLDEN CONVERSATION SUITE\n")
	b.WriteString("suite v" + itoa(s.SuiteVersion) + " · model " + s.Model + " (" + s.Provider + ") · " + s.At + "\n\n")
	b.WriteString("OVERALL: " + verdictWord(s) + "\n")
	b.WriteString("Cases: " + itoa(s.Total) + "  pass " + itoa(s.Passed) + "  fail " + itoa(s.Failed) +
		"  skip " + itoa(s.Skipped) + "  hard-fails " + itoa(s.HardFails) +
		"  (" + fmtMillis(s.DurationMs) + ")\n\n")
	// Category rows.
	keys := make([]string, 0, len(s.ByCategory))
	for k := range s.ByCategory {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	for _, k := range keys {
		cs := s.ByCategory[Category(k)]
		b.WriteString(strings.ToUpper(k) + "  " + itoa(cs.Passed) + "/" + itoa(cs.Total) + " pass")
		if cs.HardFails > 0 {
			b.WriteString("  HARD-FAILS " + itoa(cs.HardFails))
		}
		b.WriteString("\n")
	}
	// List failed/skipped cases.
	b.WriteString("\n")
	for _, r := range s.Results {
		mark := "✓"
		switch r.Verdict {
		case VerdictFail:
			mark = "✗"
		case VerdictSkip:
			mark = "○"
		}
		if r.Verdict == VerdictPass && !hasHardFailA(r.Assertions) {
			continue
		}
		line := mark + " " + r.ID + " [" + string(r.Category) + "] " + r.Title
		if r.Verdict == VerdictFail {
			line += " — " + string(r.Classification)
			if r.Error != "" {
				line += " (" + clip(r.Error, 160) + ")"
			}
		}
		if r.Verdict == VerdictSkip {
			line += " (NOT RUN)"
		}
		b.WriteString(line + "\n")
		for _, a := range r.Assertions {
			if !a.Pass {
				b.WriteString("    - " + a.Name + ": " + clip(a.Detail, 140) + "\n")
			}
		}
	}
	return b.String()
}

func verdictWord(s Summary) string {
	if s.Failed > 0 {
		return "FAIL"
	}
	if s.Skipped > 0 {
		return "PASS (with SKIPPED/NOT-RUN cases)"
	}
	return "PASS"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func fmtMillis(ms int64) string {
	if ms < 1000 {
		return itoa(int(ms)) + "ms"
	}
	return itoa(int(ms/1000)) + "s"
}

// CompareSummary renders model-vs-previous deltas per category.
func CompareSummary(prev, cur Summary) string {
	var b strings.Builder
	b.WriteString("\nGOLDEN COMPARE (suite v" + itoa(prev.SuiteVersion) + " → v" + itoa(cur.SuiteVersion) + ")\n")
	b.WriteString(prev.Provider + "/" + prev.Model + " → " + cur.Provider + "/" + cur.Model + "\n")
	b.WriteString("passed " + itoa(prev.Passed) + " → " + itoa(cur.Passed) +
		"   failed " + itoa(prev.Failed) + " → " + itoa(cur.Failed) +
		"   hard " + itoa(prev.HardFails) + " → " + itoa(cur.HardFails) + "\n")
	return b.String()
}

// hasHardFailA reports whether any assertion is a failing hard gate.
func hasHardFailA(as []AssertionResult) bool {
	for _, a := range as {
		if a.Hard && !a.Pass {
			return true
		}
	}
	return false
}
