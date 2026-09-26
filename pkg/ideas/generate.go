package ideas

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/providers"
)

// Phase B: model-drafted ideas with verified citations. The model proposes
// in plain warm language for a non-technical owner; every factual claim
// must carry a [memory:<id>] or [routine:<runid>] marker drawn from the
// supplied evidence. Markers that do not resolve render the draft
// "unverified" by construction — the evidence decides, never the prose.

// DraftPrompt is the strict drafting contract.
const DraftPrompt = `You suggest small helpful ideas for the owner of a personal AI. Rules:
1. Suggest at most 3 ideas, each as: "Title: <short title>" on its own line, then "Why: <one or two sentences>" on the next lines.
2. EVERY factual claim about the owner must cite its evidence inline with [memory:<id>] or [routine:<runid>] markers, using exactly the ids from the evidence list. A claim without a marker is a failure.
3. Plain warm language for a non-technical owner. No jargon, no markdown headers, no bullet lists.
4. Never suggest anything consequential without saying the owner confirms first.
5. If the evidence supports nothing worth suggesting, reply with exactly: NONE`

var markerRe = regexp.MustCompile(`\[(memory|routine):([^\]\s]+)\]`)

// Draft is one parsed model suggestion.
type Draft struct {
	Title string
	Body  string
}

// ParseDrafts splits model text into Title/Why blocks. "NONE" (or empty)
// yields no drafts.
func ParseDrafts(text string) []Draft {
	if strings.TrimSpace(strings.ToUpper(text)) == "NONE" {
		return nil
	}
	var out []Draft
	var cur *Draft
	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(strings.ToUpper(t), "TITLE:") {
			out = append(out, Draft{Title: strings.TrimSpace(t[len("Title:"):])})
			cur = &out[len(out)-1]
			continue
		}
		if strings.HasPrefix(strings.ToUpper(t), "WHY:") {
			if cur == nil {
				out = append(out, Draft{})
				cur = &out[len(out)-1]
			}
			cur.Body = strings.TrimSpace(t[len("Why:"):])
			continue
		}
		if cur != nil && t != "" {
			if cur.Body != "" {
				cur.Body += " "
			}
			cur.Body += t
		}
	}
	var kept []Draft
	for _, d := range out {
		if strings.TrimSpace(d.Title) != "" {
			kept = append(kept, d)
		}
	}
	return kept
}

// Evidence is what the model may cite, keyed by marker id.
type Evidence struct {
	Memories map[string]MemoryFact
	Runs     map[string]RoutineRun
}

// VerifyDraft resolves a draft's markers against evidence, producing an
// Idea. Markers that do not resolve, or a body with factual claims but no
// markers at all, mark the idea Unverified.
func VerifyDraft(d Draft, ev Evidence, now time.Time) Idea {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	idea := Idea{
		ID:        newID(),
		Title:     strings.TrimSpace(d.Title),
		Body:      strings.TrimSpace(d.Body),
		Status:    StatusPending,
		CreatedAt: now,
	}
	seen := map[string]bool{}
	for _, m := range markerRe.FindAllStringSubmatch(idea.Body+" "+idea.Title, -1) {
		kind, id := m[1], m[2]
		key := kind + ":" + id
		if seen[key] {
			continue
		}
		seen[key] = true
		switch kind {
		case "memory":
			if f, ok := ev.Memories[id]; ok {
				idea.Sources = append(idea.Sources, Source{Kind: SourceMemory, Ref: id,
					Excerpt: fmt.Sprintf("%s: %s", f.Predicate, truncate(f.Value, 80))})
			} else {
				idea.Unverified = true
			}
		case "routine":
			if r, ok := ev.Runs[id]; ok {
				idea.Sources = append(idea.Sources, Source{Kind: SourceRoutine, Ref: id,
					Excerpt: fmt.Sprintf("run status=%s", r.Status)})
			} else {
				idea.Unverified = true
			}
		}
	}
	if len(seen) == 0 && hasFactualClaim(idea.Body) {
		idea.Unverified = true
	}
	// Markers are evidence bookkeeping, not user prose: strip them.
	idea.Title = markerRe.ReplaceAllString(idea.Title, "")
	idea.Body = markerRe.ReplaceAllString(idea.Body, "")
	idea.Title = strings.TrimSpace(idea.Title)
	idea.Body = strings.Join(strings.Fields(idea.Body), " ")
	return idea
}

// hasFactualClaim is a conservative heuristic: sentences with personal
// content words that cite nothing. Short greetings and questions pass.
func hasFactualClaim(body string) bool {
	b := strings.ToLower(strings.TrimSpace(body))
	if b == "" || strings.HasSuffix(b, "?") {
		return false
	}
	for _, w := range []string{"you ", "your ", "remember", "prefer", "routine", "goal", "drink", "live", "like"} {
		if strings.Contains(b, w) {
			return true
		}
	}
	return false
}

// DraftIdeas calls the provider with the drafting contract over serialized
// evidence. Thinking stays off; temperature 0 for determinism.
func DraftIdeas(ctx context.Context, p providers.LLMProvider, model, evidence string) (string, error) {
	resp, err := p.Chat(ctx, []providers.Message{
		{Role: "system", Content: DraftPrompt},
		{Role: "user", Content: evidence},
	}, nil, model, map[string]interface{}{"temperature": 0.0, "thinking_level": "off"})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}
