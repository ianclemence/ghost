package cards

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Spec is the model's JSON surface for card production. The model emits
// specs; the catalog renders validated Cards. The model never constructs
// a Card directly, so prompt-injected UI cannot smuggle kinds, actions,
// or data the catalog did not allow.
type Spec struct {
	Kind    Kind                   `json:"kind"`
	Title   string                 `json:"title"`
	Body    string                 `json:"body,omitempty"`
	Topic   string                 `json:"topic,omitempty"`
	Data    map[string]interface{} `json:"data,omitempty"`
	Actions []Action               `json:"actions,omitempty"`
}

// kindSchema is the guardrail per card kind.
type kindSchema struct {
	requireTopic bool
	requireData  []string
	maxActions   int
	styles       map[string]bool
}

var catalog = map[Kind]kindSchema{
	KindSuggestion:  {requireTopic: true, maxActions: 2, styles: map[string]bool{"primary": true, "secondary": true}},
	KindGoalUpdate:  {requireData: []string{"goal_id"}, maxActions: 2, styles: map[string]bool{"primary": true, "secondary": true}},
	KindCart:        {requireData: []string{"items"}, maxActions: 4, styles: map[string]bool{"primary": true, "secondary": true, "destructive": true}},
	KindBrowserView: {requireData: []string{"url"}, maxActions: 2, styles: map[string]bool{"primary": true, "secondary": true}},
	// Memory receipts carry no actions: explaining is read-only, and
	// forgetting stays an explicit owner act on the Memory screen.
	KindMemoryReceipt: {requireData: []string{"claim_id"}, maxActions: 0},
}

// RenderSpec validates a model-emitted spec against the catalog and
// builds the Card. Unknown kinds, missing fields, oversized action
// sets, unlisted styles, and non-scalar data all fail closed.
func RenderSpec(raw []byte) (Card, error) {
	var spec Spec
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		return Card{}, fmt.Errorf("card spec: %w", err)
	}
	schema, ok := catalog[spec.Kind]
	if !ok {
		return Card{}, fmt.Errorf("card spec: kind %q not in catalog", spec.Kind)
	}
	if strings.TrimSpace(spec.Title) == "" {
		return Card{}, fmt.Errorf("card spec: title is required")
	}
	if schema.requireTopic && strings.TrimSpace(spec.Topic) == "" {
		return Card{}, fmt.Errorf("card spec: %s requires a topic", spec.Kind)
	}
	for _, key := range schema.requireData {
		v, ok := spec.Data[key]
		if !ok || v == nil || v == "" {
			return Card{}, fmt.Errorf("card spec: %s requires data.%s", spec.Kind, key)
		}
	}
	if len(spec.Actions) > schema.maxActions {
		return Card{}, fmt.Errorf("card spec: %s carries at most %d actions", spec.Kind, schema.maxActions)
	}
	seen := map[string]bool{}
	for _, a := range spec.Actions {
		if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.Label) == "" {
			return Card{}, fmt.Errorf("card spec: actions need id and label")
		}
		if seen[a.ID] {
			return Card{}, fmt.Errorf("card spec: duplicate action id %q", a.ID)
		}
		seen[a.ID] = true
		if a.Style != "" && !schema.styles[a.Style] {
			return Card{}, fmt.Errorf("card spec: style %q not allowed for %s", a.Style, spec.Kind)
		}
	}
	// Data values stay scalar: no nested objects or arrays smuggled
	// through except the explicitly required ones (cart items).
	for k, v := range spec.Data {
		if k == "items" && spec.Kind == KindCart {
			continue
		}
		switch v.(type) {
		case string, bool, float64, int, int64, json.Number, nil:
		default:
			return Card{}, fmt.Errorf("card spec: data.%s must be scalar", k)
		}
	}
	c := Card{ID: newID(), Kind: spec.Kind, Title: strings.TrimSpace(spec.Title),
		Body: strings.TrimSpace(spec.Body), Topic: strings.TrimSpace(spec.Topic),
		Data: spec.Data, Actions: spec.Actions,
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(24 * time.Hour)}
	if err := c.Validate(); err != nil {
		return Card{}, err
	}
	return c, nil
}

// ActionRecord is one card action taken, for the inspector and audit.
type ActionRecord struct {
	At        time.Time `json:"at"`
	CardID    string    `json:"card_id"`
	CardKind  string    `json:"card_kind"`
	ActionID  string    `json:"action_id"`
	RequestID string    `json:"request_id,omitempty"`
	Actor     string    `json:"actor"`
	Result    string    `json:"result"`
}

// ActionLog is the append-only log of card actions. Bounded; the
// inspector reads recent entries. Approvals still resolve through the
// broker — the log records, never authorizes.
type ActionLog struct {
	mu      sync.Mutex
	entries []ActionRecord
}

const actionLogCap = 200

// DefaultActionLog is the process-wide log.
var DefaultActionLog = &ActionLog{}

// Record appends one action outcome.
func (l *ActionLog) Record(r ActionRecord) {
	if r.At.IsZero() {
		r.At = time.Now().UTC()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, r)
	if len(l.entries) > actionLogCap {
		l.entries = l.entries[len(l.entries)-actionLogCap:]
	}
}

// Recent returns the last n entries, newest first.
func (l *ActionLog) Recent(n int) []ActionRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n > len(l.entries) {
		n = len(l.entries)
	}
	out := make([]ActionRecord, n)
	for i := 0; i < n; i++ {
		out[i] = l.entries[len(l.entries)-1-i]
	}
	return out
}

// CatalogKinds lists producible kinds for prompts and docs.
func CatalogKinds() []Kind {
	return []Kind{KindSuggestion, KindGoalUpdate, KindCart, KindBrowserView}
}
