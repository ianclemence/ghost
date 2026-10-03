package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/ianclemence/ghost/pkg/personalcontext"
)

// MemoryCorrectTool lets the model find, correct or forget one structured
// belief in Personal Context on the owner's word.
//
// Extraction only ever adds what it can read out of a sentence. It cannot
// carry out "the trip is on the 16th now, forget the 26th", because that
// names a belief to change rather than stating a new one. This tool is that
// act, with the same rule the owner would expect from a person: say what is
// about to change, and change it only once the owner has said so.
//
// The preview step is structural, not advisory: nothing is written unless
// confirmed is true, and the preview text tells the model to ask unless the
// owner has just stated the correction in their own message.
type MemoryCorrectTool struct {
	store *personalcontext.Store
	// Scopes resolves the memory scopes the calling session may see. Nil
	// means unfiltered (unit tests). The model can never set it.
	Scopes func(session string) []string
}

func NewMemoryCorrectTool(store *personalcontext.Store) *MemoryCorrectTool {
	return &MemoryCorrectTool{store: store}
}

func (t *MemoryCorrectTool) Name() string { return "memory_correct" }

func (t *MemoryCorrectTool) Description() string {
	return "Find, correct or forget one thing Ghost believes about the owner (a trip's dates, where they live, a plan). " +
		"action=find with about=<words>: lists matching beliefs with their ids and when the owner said them. " +
		"action=update with entry_id and new_value: changes that belief. action=forget with entry_id: drops it. " +
		"Always find first (unless an Unconfirmed changes note already gave you the ids). action=dismiss with entry_id drops an unconfirmed candidate the owner said was wrong. Update and forget only PREVIEW (change nothing) until you pass confirmed=true; pass it only when " +
		"the owner has just told you the new fact or said yes to your preview. If a new message merely seems to disagree with a stored belief, " +
		"ask which is right instead of changing it. After a change, tell the owner in one line what it was and what it is now."
}

func (t *MemoryCorrectTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":    map[string]interface{}{"type": "string", "enum": []string{"find", "update", "forget", "dismiss"}},
			"about":     map[string]interface{}{"type": "string", "description": "With find: words that identify the belief, e.g. \"shenzhen trip dates\"."},
			"entry_id":  map[string]interface{}{"type": "string", "description": "With update or forget: the id from find."},
			"new_value": map[string]interface{}{"type": "string", "description": "With update: the corrected fact as a short sentence, in the owner's terms (\"Their Shenzhen trip starts on 16 October\")."},
			"confirmed": map[string]interface{}{"type": "boolean", "description": "true only when the owner stated the correction themselves or said yes to your preview."},
		},
		"required": []string{"action"},
	}
}

func (t *MemoryCorrectTool) visible(session string) []personalcontext.Entry {
	if t.Scopes != nil {
		return t.store.CurrentInScope(t.Scopes(session))
	}
	return t.store.Current()
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !(unicode.IsLetter(r) || unicode.IsDigit(r)) })
}

var correctStop = map[string]bool{"the": true, "a": true, "an": true, "my": true, "of": true, "to": true, "and": true, "is": true}

// matchScore counts how many of the query's words appear in the belief.
func matchScore(query []string, e personalcontext.Entry) int {
	hay := strings.ToLower(e.Predicate + " " + personalcontext.Value(e) + " " + e.Quote)
	n := 0
	for _, w := range query {
		if len(w) < 3 || correctStop[w] {
			continue
		}
		if strings.Contains(hay, w) {
			n++
		}
	}
	return n
}

func describeBelief(e personalcontext.Entry) string {
	when := e.CreatedAt.Format("2 Jan")
	return fmt.Sprintf("%s: %q (told %s)", e.ID, personalcontext.Value(e), when)
}

func (t *MemoryCorrectTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	if t.store == nil {
		return ErrorResult("memory_correct unavailable: personal context store not configured")
	}
	action, _ := args["action"].(string)
	session := SessionKeyFromContext(ctx)
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "find":
		return t.find(session, sarg(args, "about"))
	case "update":
		return t.change(ctx, session, args, false)
	case "forget":
		return t.change(ctx, session, args, true)
	case "dismiss":
		return t.dismiss(session, sarg(args, "entry_id"))
	}
	return ErrorResult("action must be find, update, forget or dismiss")
}

func (t *MemoryCorrectTool) find(session, about string) *ToolResult {
	q := words(about)
	if len(q) == 0 {
		return ErrorResult("about is required: the words that identify the belief")
	}
	type hit struct {
		e     personalcontext.Entry
		score int
	}
	var hits []hit
	for _, e := range t.visible(session) {
		if s := matchScore(q, e); s > 0 {
			hits = append(hits, hit{e, s})
		}
	}
	if len(hits) == 0 {
		return NewToolResult("Nothing stored matches that. Say so; do not guess what was stored.")
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].e.CreatedAt.After(hits[j].e.CreatedAt)
	})
	if len(hits) > 5 {
		hits = hits[:5]
	}
	var b strings.Builder
	b.WriteString("Matching beliefs:")
	for _, h := range hits {
		b.WriteString("\n- " + describeBelief(h.e))
	}
	return NewToolResult(b.String())
}

// dismiss drops an unconfirmed candidate the owner said was wrong. The belief
// in force is untouched.
func (t *MemoryCorrectTool) dismiss(session, id string) *ToolResult {
	e, ok := t.store.Get(id)
	if !ok || e.Status != personalcontext.StatusUncertain {
		return ErrorResult("No unconfirmed change has that id.")
	}
	if !personalcontext.VisibleTo(e, t.scopesFor(session)) {
		return ErrorResult("That belongs to another context and is not visible here.")
	}
	if err := t.store.Forget(id); err != nil {
		return ErrorResult(fmt.Sprintf("Could not dismiss that: %v", err))
	}
	return NewToolResult("Dismissed. What I had stored stays as it was.")
}

func (t *MemoryCorrectTool) scopesFor(session string) []string {
	if t.Scopes != nil {
		return t.Scopes(session)
	}
	return nil
}

// clearCandidates drops unconfirmed candidates for a belief the owner just
// settled, so the same question is not asked again.
func (t *MemoryCorrectTool) clearCandidates(subject, predicate string) {
	for _, e := range t.store.ByPredicate(predicate) {
		if e.Subject == subject && e.Status == personalcontext.StatusUncertain {
			_ = t.store.Forget(e.ID)
		}
	}
}

func (t *MemoryCorrectTool) change(ctx context.Context, session string, args map[string]interface{}, forget bool) *ToolResult {
	id := sarg(args, "entry_id")
	cur, ok := t.store.Get(id)
	if !ok || cur.Status != personalcontext.StatusCurrent {
		return ErrorResult("No current belief has that id. Run find first.")
	}
	visible := false
	for _, e := range t.visible(session) {
		if e.ID == cur.ID {
			visible = true
			break
		}
	}
	if !visible {
		return ErrorResult("That belief belongs to another context and is not visible here.")
	}
	confirmed, _ := args["confirmed"].(bool)
	old := personalcontext.Value(cur)

	if forget {
		if !confirmed {
			return NewToolResult(fmt.Sprintf("Nothing changed yet. I would forget: %q. Ask the owner to confirm, then call again with confirmed=true.", old))
		}
		if _, err := personalcontext.ForgetPipelineWith(t.store, t.store.Workspace(), cur.ID, "the owner asked Ghost to forget it"); err != nil {
			return ErrorResult(fmt.Sprintf("Could not forget that: %v", err))
		}
		t.clearCandidates(cur.Subject, cur.Predicate)
		return NewToolResult(fmt.Sprintf("Forgot: %q.", old))
	}

	nv := strings.TrimSpace(sarg(args, "new_value"))
	if nv == "" {
		return ErrorResult("new_value is required for update")
	}
	if strings.EqualFold(nv, old) {
		return NewToolResult("That is already what I have stored. Nothing changed.")
	}
	if !confirmed {
		return NewToolResult(fmt.Sprintf("Nothing changed yet. Stored: %q (told %s). Would become: %q. "+
			"Unless the owner just said this themselves, ask which is right; then call again with confirmed=true.",
			old, cur.CreatedAt.Format("2 Jan"), nv))
	}
	if _, _, err := t.store.Correct(cur.ID, nv, RequestMessage(ctx), session); err != nil {
		return ErrorResult(fmt.Sprintf("Could not update that: %v", err))
	}
	return NewToolResult(fmt.Sprintf("Updated. Was: %q. Now: %q.", old, nv))
}
