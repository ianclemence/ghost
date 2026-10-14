package golden

import (
	"crypto/sha1"
	"fmt"
	"sort"
	"strings"
)

type CaseTrials struct {
	ID       string
	Title    string
	Category Category
	Trials   int
	Passes   int
}

func (c CaseTrials) PassAtK() bool  { return c.Passes >= 1 }
func (c CaseTrials) PassHatK() bool { return c.Trials > 0 && c.Passes == c.Trials }
func (c CaseTrials) Flaky() bool    { return c.Passes > 0 && c.Passes < c.Trials }

type TrialReport struct {
	Model    string
	Provider string
	K        int
	Cases    []CaseTrials
}

// PassAtK is the share of cases that passed at least once in K trials.
func (r TrialReport) PassAtK() float64 {
	return r.share(CaseTrials.PassAtK)
}

// PassHatK is the share of cases that passed in every trial: the number that
// says whether Ghost can be relied on rather than merely able.
func (r TrialReport) PassHatK() float64 {
	return r.share(CaseTrials.PassHatK)
}

func (r TrialReport) share(f func(CaseTrials) bool) float64 {
	if len(r.Cases) == 0 {
		return 0
	}
	n := 0
	for _, c := range r.Cases {
		if f(c) {
			n++
		}
	}
	return float64(n) / float64(len(r.Cases))
}

func (r TrialReport) Flaky() []CaseTrials {
	var out []CaseTrials
	for _, c := range r.Cases {
		if c.Flaky() {
			out = append(out, c)
		}
	}
	return out
}

// AggregateTrials folds repeated runs of one suite into per-case counts.
// Skipped cases carry no signal and are left out.
func AggregateTrials(runs []Summary) TrialReport {
	rep := TrialReport{K: len(runs)}
	byID := map[string]*CaseTrials{}
	var order []string
	for _, s := range runs {
		if rep.Model == "" {
			rep.Model, rep.Provider = s.Model, s.Provider
		}
		for _, cr := range s.Results {
			if cr.Verdict == VerdictSkip {
				continue
			}
			c, ok := byID[cr.ID]
			if !ok {
				c = &CaseTrials{ID: cr.ID, Title: cr.Title, Category: cr.Category}
				byID[cr.ID] = c
				order = append(order, cr.ID)
			}
			c.Trials++
			if cr.Verdict == VerdictPass && !hasHardFail(cr) {
				c.Passes++
			}
		}
	}
	for _, id := range order {
		rep.Cases = append(rep.Cases, *byID[id])
	}
	return rep
}

func RenderTrials(r TrialReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\nTRIALS  %d per case · model %s (%s)\n", r.K, r.Model, r.Provider)
	fmt.Fprintf(&b, "pass@%d  %.0f%%   pass^%d  %.0f%%   (%d cases)\n", r.K, 100*r.PassAtK(), r.K, 100*r.PassHatK(), len(r.Cases))
	flaky := r.Flaky()
	sort.Slice(flaky, func(i, j int) bool { return flaky[i].ID < flaky[j].ID })
	if len(flaky) == 0 {
		b.WriteString("No flaky cases.\n")
		return b.String()
	}
	b.WriteString("Flaky (passed some trials, not all):\n")
	for _, c := range flaky {
		fmt.Fprintf(&b, "  %-8s %d/%d  %s\n", c.ID, c.Passes, c.Trials, c.Title)
	}
	return b.String()
}

type Split string

const (
	SplitAll     Split = ""
	SplitDev     Split = "dev"
	SplitHoldout Split = "holdout"
)

// InHoldout puts roughly one case in five aside, decided by the case id alone,
// so the same cases stay held out across runs and prompt edits are never
// tuned against them.
func InHoldout(id string) bool {
	h := sha1.Sum([]byte(id))
	return h[0]%5 == 0
}

func FilterSplit(suite []Conversation, split Split) []Conversation {
	if split == SplitAll {
		return suite
	}
	var out []Conversation
	for _, c := range suite {
		if InHoldout(c.ID) == (split == SplitHoldout) {
			out = append(out, c)
		}
	}
	return out
}
