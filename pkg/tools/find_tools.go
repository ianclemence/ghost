package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"
)

type turnToolsKey struct{}

// TurnTools is the set of tools offered for one turn, and the way to widen it.
type TurnTools struct {
	Offered *ToolRegistry
	Profile ToolProfile
}

func WithTurnTools(ctx context.Context, t *TurnTools) context.Context {
	return context.WithValue(ctx, turnToolsKey{}, t)
}

func turnToolsFrom(ctx context.Context) *TurnTools {
	t, _ := ctx.Value(turnToolsKey{}).(*TurnTools)
	return t
}

const findToolsLimit = 5

type FindToolsTool struct {
	all *ToolRegistry
	mu  sync.Mutex
}

func NewFindToolsTool(all *ToolRegistry) *FindToolsTool { return &FindToolsTool{all: all} }

func (t *FindToolsTool) Name() string { return "find_tools" }

func (t *FindToolsTool) Description() string {
	return "Look for a tool you were not given for this request. Use it only when the owner asks for something Ghost may be able to do and none of your tools fits " +
		"(\"turn off the lights\", \"what's playing\", \"check my flight\"). Say in a few words what you need done. The best matches become available to you straight away."
}

func (t *FindToolsTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"need": map[string]interface{}{"type": "string", "description": "What the tool must do, in a few English words"},
		},
		"required": []string{"need"},
	}
}

func tokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

var searchStop = map[string]bool{"the": true, "a": true, "an": true, "to": true, "of": true, "my": true, "me": true, "for": true, "and": true, "or": true, "in": true, "on": true, "is": true, "it": true, "that": true, "this": true, "with": true, "use": true, "when": true, "your": true, "you": true, "i": true}

type scored struct {
	name  string
	score float64
	desc  string
}

func firstSentence(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, ".\n"); i > 0 {
		s = s[:i]
	}
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// rankTools scores each candidate by how many of the need's words appear in
// its name (heavily) and description, preferring rarer words.
func rankTools(need string, candidates map[string]Tool) []scored {
	var q []string
	for _, w := range tokens(need) {
		if !searchStop[w] && len(w) > 1 {
			q = append(q, w)
		}
	}
	if len(q) == 0 {
		return nil
	}
	docFreq := map[string]int{}
	docs := map[string][]string{}
	names := map[string][]string{}
	for n, tool := range candidates {
		d := tokens(tool.Description())
		docs[n] = d
		names[n] = tokens(n)
		seen := map[string]bool{}
		for _, w := range append(append([]string{}, d...), names[n]...) {
			if !seen[w] {
				seen[w] = true
				docFreq[w]++
			}
		}
	}
	total := float64(len(candidates))
	var out []scored
	for n, tool := range candidates {
		var s float64
		for _, w := range q {
			idf := 1.0 + total/(1.0+float64(docFreq[w]))/total*10
			for _, x := range names[n] {
				if x == w || strings.HasPrefix(x, w) || strings.HasPrefix(w, x) && len(x) > 2 {
					s += 3 * idf
				}
			}
			for _, x := range docs[n] {
				if x == w {
					s += idf
				}
			}
		}
		if s > 0 {
			out = append(out, scored{name: n, score: s, desc: firstSentence(tool.Description(), 120)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].name < out[j].name
	})
	return out
}

func (t *FindToolsTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	need, _ := args["need"].(string)
	if strings.TrimSpace(need) == "" {
		return ErrorResult("need is required: say what the tool must do")
	}
	turn := turnToolsFrom(ctx)
	if turn == nil || turn.Offered == nil {
		return ErrorResult("tool search is not available here")
	}
	candidates := map[string]Tool{}
	for _, name := range FilterNamesByProfile(turn.Profile, t.all.List()) {
		if _, have := turn.Offered.Get(name); have || t.all.IsHidden(name) || name == t.Name() {
			continue
		}
		if tool, ok := t.all.Get(name); ok {
			candidates[name] = tool
		}
	}
	ranked := rankTools(need, candidates)
	if len(ranked) == 0 {
		return NewToolResult(fmt.Sprintf("No other tool matches %q. Say plainly that you can't do that from here.", need))
	}
	if len(ranked) > findToolsLimit {
		ranked = ranked[:findToolsLimit]
	}
	var b strings.Builder
	b.WriteString("Now available to you, call them directly:\n")
	for _, r := range ranked {
		turn.Offered.Register(candidates[r.name])
		fmt.Fprintf(&b, "- %s: %s\n", r.name, r.desc)
	}
	return &ToolResult{ForLLM: strings.TrimSpace(b.String()), Silent: true}
}
