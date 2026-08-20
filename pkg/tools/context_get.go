package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ianclemence/ghost/pkg/personalcontext"
)

// ContextGetTool is the on-demand, structured lookup tool for Personal
// Context. It answers "what does Ghost currently believe?" for a specific
// kind, subject, or predicate by reading the current state of the
// personalcontext.Store. It never runs automatic injection, never touches the
// conversation store, and never queries RAG: facts are queried structurally.
type ContextGetTool struct {
	store *personalcontext.Store
}

func NewContextGetTool(store *personalcontext.Store) *ContextGetTool {
	return &ContextGetTool{store: store}
}

func (t *ContextGetTool) Name() string {
	return "context_get"
}

func (t *ContextGetTool) Description() string {
	return "Query Personal Context: what Ghost currently believes about the user, as structured entries. " +
		"Filter by kind (identity, fact, preference, relationship, goal, decision, consent, routine), " +
		"subject (e.g. user), or predicate (e.g. preference/favorite_color, fact/location). " +
		"Only current entries are returned; superseded, rejected, and expired entries are excluded, and " +
		"conflicting or uncertain entries are reported separately rather than presented as facts. " +
		"Requires at least one of kind, subject, or predicate."
}

func (t *ContextGetTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"kind": map[string]interface{}{
				"type":        "string",
				"description": "Filter by entry kind: identity, fact, preference, relationship, goal, decision, consent, routine.",
			},
			"subject": map[string]interface{}{
				"type":        "string",
				"description": "Filter by entry subject (e.g. \"user\").",
			},
			"predicate": map[string]interface{}{
				"type":        "string",
				"description": "Filter by exact predicate (e.g. \"fact/location\", \"preference/favorite_color\").",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of entries to return (default 20, max 50).",
				"default":     20,
			},
		},
		"required": []string{},
	}
}

// contextGetQuery echoes the filters the model supplied so the result is
// unambiguous about what was asked.
type contextGetQuery struct {
	Kind      string `json:"kind,omitempty"`
	Subject   string `json:"subject,omitempty"`
	Predicate string `json:"predicate,omitempty"`
}

// contextGetPayload is the structured result. Entries are the current facts;
// unresolved holds conflicting/uncertain entries, which are never presented
// as current answers. Provenance travels with each entry untouched.
type contextGetPayload struct {
	Query      contextGetQuery         `json:"query"`
	Count      int                     `json:"count"`
	Entries    []personalcontext.Entry `json:"entries"`
	Unresolved []personalcontext.Entry `json:"unresolved,omitempty"`
	Note       string                  `json:"note,omitempty"`
}

const (
	defaultContextGetLimit = 20
	maxContextGetLimit     = 50
)

func (t *ContextGetTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	if t.store == nil {
		return ErrorResult("context_get unavailable: personal context store not configured")
	}

	kind, _ := args["kind"].(string)
	subject, _ := args["subject"].(string)
	predicate, _ := args["predicate"].(string)

	if kind == "" && subject == "" && predicate == "" {
		return ErrorResult("at least one filter is required: kind, subject, or predicate")
	}
	if kind != "" && !personalcontext.ValidKind(personalcontext.Kind(kind)) {
		return ErrorResult(fmt.Sprintf("invalid kind %q; valid kinds: identity, fact, preference, relationship, goal, decision, consent, routine", kind))
	}

	limit := defaultContextGetLimit
	if raw, ok := args["limit"].(float64); ok {
		limit = int(raw)
	}
	if limit <= 0 {
		limit = defaultContextGetLimit
	}
	if limit > maxContextGetLimit {
		limit = maxContextGetLimit
	}

	current, unresolved := t.query(time.Now(), personalcontext.Kind(kind), subject, predicate)
	if len(current) > limit {
		current = current[:limit]
	}
	if len(unresolved) > limit {
		unresolved = unresolved[:limit]
	}

	payload := contextGetPayload{
		Query:      contextGetQuery{Kind: kind, Subject: subject, Predicate: predicate},
		Count:      len(current),
		Entries:    current,
		Unresolved: unresolved,
	}
	if len(unresolved) > 0 {
		word := "entries"
		if len(unresolved) == 1 {
			word = "entry"
		}
		payload.Note = fmt.Sprintf(
			"%d unresolved %s (conflicting or uncertain) match this query and are listed under \"unresolved\"; do not treat them as facts.",
			len(unresolved), word)
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return ErrorResult(fmt.Sprintf("context_get marshal failed: %v", err)).WithError(err)
	}
	return SilentResult(string(raw))
}

// query partitions matching entries into current facts and unresolved state.
// Current facts reuse the store's own semantics (status current and temporal
// validity, via CurrentAt) so validity logic is never reimplemented here.
// Unresolved state is read from the status-agnostic store queries, which also
// carry provenance, and surfaced explicitly instead of being silently chosen.
func (t *ContextGetTool) query(now time.Time, kind personalcontext.Kind, subject, predicate string) (current, unresolved []personalcontext.Entry) {
	for _, e := range t.store.CurrentAt(now) {
		if matchesQuery(e, kind, subject, predicate) {
			current = append(current, e)
		}
	}

	var all []personalcontext.Entry
	switch {
	case predicate != "":
		all = t.store.ByPredicate(predicate)
	case subject != "":
		all = t.store.BySubject(subject)
	default:
		all = t.store.ByKind(kind)
	}
	for _, e := range all {
		if !matchesQuery(e, kind, subject, predicate) {
			continue
		}
		if e.Status == personalcontext.StatusConflicting || e.Status == personalcontext.StatusUncertain {
			unresolved = append(unresolved, e)
		}
	}
	return current, unresolved
}

func matchesQuery(e personalcontext.Entry, kind personalcontext.Kind, subject, predicate string) bool {
	if kind != "" && e.Kind != kind {
		return false
	}
	if subject != "" && e.Subject != subject {
		return false
	}
	if predicate != "" && e.Predicate != predicate {
		return false
	}
	return true
}
