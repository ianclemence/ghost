package golden

import (
	"fmt"
	"sort"
	"strings"
)

type ScoreRow struct {
	Model     string
	Provider  string
	Runs      int
	Passed    int
	Total     int
	HardFails int
	Previous  float64
	Latest    float64
	Saturated []Category
	At        string
}

func rate(p, t int) float64 {
	if t == 0 {
		return 0
	}
	return float64(p) / float64(t)
}

const saturationRuns = 3

// Scorecard rolls the run history up per model: the newest result, the change
// since the run before it, and the categories that have passed every case in
// each of the last few runs and so no longer tell anything apart.
func Scorecard(entries []HistoryEntry) []ScoreRow {
	by := map[string][]HistoryEntry{}
	var keys []string
	for _, e := range entries {
		k := e.Provider + "/" + e.Model
		if _, ok := by[k]; !ok {
			keys = append(keys, k)
		}
		by[k] = append(by[k], e)
	}
	sort.Strings(keys)
	var rows []ScoreRow
	for _, k := range keys {
		es := by[k]
		last := es[len(es)-1]
		row := ScoreRow{Model: last.Model, Provider: last.Provider, Runs: len(es), At: last.At,
			Passed: last.Summary.Passed, Total: last.Summary.Total, HardFails: last.Summary.HardFails,
			Latest: rate(last.Summary.Passed, last.Summary.Total)}
		if len(es) > 1 {
			p := es[len(es)-2].Summary
			row.Previous = rate(p.Passed, p.Total)
		}
		row.Saturated = saturated(es)
		rows = append(rows, row)
	}
	return rows
}

func saturated(es []HistoryEntry) []Category {
	if len(es) < saturationRuns {
		return nil
	}
	recent := es[len(es)-saturationRuns:]
	var out []Category
	for cat, cs := range recent[len(recent)-1].Summary.ByCategory {
		if cs.Total == 0 {
			continue
		}
		all := true
		for _, e := range recent {
			c := e.Summary.ByCategory[cat]
			if c.Total == 0 || c.Passed != c.Total {
				all = false
				break
			}
		}
		if all {
			out = append(out, cat)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func RenderScorecard(rows []ScoreRow) string {
	if len(rows) == 0 {
		return "No golden runs recorded yet.\n"
	}
	var b strings.Builder
	b.WriteString("\nGHOST MODEL SCORECARD\n\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "%s (%s)  %d/%d  %.0f%%", r.Model, r.Provider, r.Passed, r.Total, 100*r.Latest)
		if r.Runs > 1 {
			fmt.Fprintf(&b, "  %+.1f pts vs previous", 100*(r.Latest-r.Previous))
		}
		if r.HardFails > 0 {
			fmt.Fprintf(&b, "  %d HARD FAILS", r.HardFails)
		}
		fmt.Fprintf(&b, "  · %d runs · %s\n", r.Runs, r.At)
		if len(r.Saturated) > 0 {
			names := make([]string, len(r.Saturated))
			for i, c := range r.Saturated {
				names[i] = string(c)
			}
			fmt.Fprintf(&b, "  saturated, no longer separating changes: %s\n", strings.Join(names, ", "))
		}
	}
	return b.String()
}
