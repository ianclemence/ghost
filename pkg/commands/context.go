package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/personalcontext"
)

// contextHandler implements /context: the user-facing, inspection-only view of
// the current Personal Context. It reads the personalcontext.Store directly and
// never calls an LLM, never searches conversations, never touches RAG, and
// never consults MEMORY.md. Only what Ghost currently believes is shown:
// superseded, rejected, expired, and future-valid entries stay out of the
// default view (they remain inspectable through the store's history queries).
// Conflicting and uncertain entries are surfaced under "Unresolved" and are
// never presented as facts.
//
// Syntax: /context [kind|subject|predicate] [--verbose]
func contextHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.PersonalContext == nil {
		return req.Reply("Personal Context is unavailable.")
	}

	filter, verbose := parseContextArgs(req.Text)

	current, unresolved := queryContext(rt.PersonalContext, filter, time.Now())

	text := renderContext(current, unresolved, filter, verbose)
	if text == "" {
		text = "Personal Context is empty."
	}
	return req.Reply(text)
}

// parseContextArgs extracts the optional filter (kind, subject, or predicate)
// and the --verbose/-v flag. The first non-flag token is the filter; extra
// tokens are ignored.
func parseContextArgs(text string) (filter string, verbose bool) {
	for _, token := range strings.Fields(text)[1:] {
		switch {
		case token == "--verbose" || token == "-v":
			verbose = true
		case strings.HasPrefix(token, "-"):
			// Unknown flags are ignored.
		case filter == "":
			filter = token
		}
	}
	return filter, verbose
}

// queryContext partitions the store into current beliefs and unresolved
// (conflicting/uncertain) state. Current beliefs reuse the store's own
// semantics via CurrentAt, so status and temporal validity are never
// reimplemented here; unresolved state comes from the status-agnostic store
// queries, which also carry provenance.
func queryContext(store *personalcontext.Store, filter string, now time.Time) (current, unresolved []personalcontext.Entry) {
	for _, e := range store.CurrentAt(now) {
		if matchesContextFilter(e, filter) {
			current = append(current, e)
		}
	}

	var all []personalcontext.Entry
	switch {
	case filter == "":
		all = store.All()
	case personalcontext.ValidKind(personalcontext.Kind(filter)):
		all = store.ByKind(personalcontext.Kind(filter))
	case strings.Contains(filter, "/"):
		all = store.ByPredicate(filter)
	default:
		all = store.BySubject(filter)
	}
	for _, e := range all {
		if matchesContextFilter(e, filter) && isUnresolved(e) {
			unresolved = append(unresolved, e)
		}
	}
	return current, unresolved
}

func matchesContextFilter(e personalcontext.Entry, filter string) bool {
	if filter == "" {
		return true
	}
	if personalcontext.ValidKind(personalcontext.Kind(filter)) {
		return e.Kind == personalcontext.Kind(filter)
	}
	if strings.Contains(filter, "/") {
		return e.Predicate == filter
	}
	return e.Subject == filter
}

// isUnresolved reports whether an entry is a conflicting or uncertain belief:
// it exists but Ghost has not selected one value as the current truth.
func isUnresolved(e personalcontext.Entry) bool {
	return e.Status == personalcontext.StatusConflicting || e.Status == personalcontext.StatusUncertain
}

// renderContext formats the query result for a human in a terminal/chat. It
// returns "" when there is nothing to show (no current beliefs and no
// unresolved state); the caller supplies the empty-state message.
func renderContext(current, unresolved []personalcontext.Entry, filter string, verbose bool) string {
	if len(current) == 0 && len(unresolved) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("### Personal Context")
	if filter != "" {
		sb.WriteString(fmt.Sprintf(" (filtered by `%s`)", filter))
	}
	sb.WriteString("\n")

	if len(current) == 0 {
		sb.WriteString("\nNo current beliefs.\n")
	} else if verbose {
		sb.WriteString("\n**Current**\n")
		renderVerboseEntries(&sb, current)
	} else {
		renderGrouped(&sb, current)
	}

	if len(unresolved) > 0 {
		sb.WriteString("\n**Unresolved**\n")
		if verbose {
			renderVerboseEntries(&sb, unresolved)
		} else {
			renderUnresolvedGroups(&sb, unresolved)
		}
		sb.WriteString("Ghost has not resolved these conflicts — no candidate is treated as a fact.\n")
	}

	return strings.TrimRight(sb.String(), "\n")
}

// renderGrouped emits current entries grouped by kind, using the predicate
// suffix as the label (the kind is already the section header).
func renderGrouped(sb *strings.Builder, current []personalcontext.Entry) {
	for _, kind := range contextDisplayOrder {
		var lines []string
		for _, e := range current {
			if e.Kind != kind {
				continue
			}
			lines = append(lines, fmt.Sprintf("- %s: %s", shortPredicate(e.Predicate), renderContextValue(e)))
		}
		if len(lines) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("\n**%s**\n", contextDisplayKinds[kind]))
		sb.WriteString(strings.Join(lines, "\n"))
		sb.WriteString("\n")
	}
}

// renderUnresolvedGroups emits conflicting/uncertain entries grouped by
// subject+predicate, listing each candidate value. The full predicate is shown
// so the user sees exactly which belief is contested.
func renderUnresolvedGroups(sb *strings.Builder, unresolved []personalcontext.Entry) {
	for _, group := range groupUnresolved(unresolved) {
		sb.WriteString(fmt.Sprintf("- %s\n", group.predicate))
		for _, v := range group.values {
			sb.WriteString(fmt.Sprintf("  - %s\n", v))
		}
	}
}

type unresolvedGroup struct {
	subject   string
	predicate string
	values    []string
}

func groupUnresolved(unresolved []personalcontext.Entry) []unresolvedGroup {
	var groups []unresolvedGroup
	var current *unresolvedGroup
	for _, e := range unresolved {
		if current == nil || current.subject != e.Subject || current.predicate != e.Predicate {
			groups = append(groups, unresolvedGroup{subject: e.Subject, predicate: e.Predicate})
			current = &groups[len(groups)-1]
		}
		current.values = append(current.values, renderContextValue(e))
	}
	return groups
}

// renderVerboseEntries emits full, auditable detail for every entry: every
// field the store carries, plus each source. This is the inspectability
// surface the architecture requires; provenance is shown exactly as stored.
func renderVerboseEntries(sb *strings.Builder, entries []personalcontext.Entry) {
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("- %s\n", e.Predicate))
		writeContextField(sb, "id", e.ID)
		writeContextField(sb, "kind", string(e.Kind))
		writeContextField(sb, "subject", e.Subject)
		writeContextField(sb, "predicate", e.Predicate)
		writeContextField(sb, "value", renderContextValue(e))
		writeContextField(sb, "status", string(e.Status))
		writeContextField(sb, "confidence", fmt.Sprintf("%.2f", e.Confidence))
		writeContextField(sb, "valid_from", formatContextTimePtr(e.ValidFrom))
		writeContextField(sb, "valid_until", formatContextTimePtr(e.ValidUntil))
		if e.SupersededBy != nil {
			writeContextField(sb, "superseded_by", *e.SupersededBy)
		}
		writeContextField(sb, "created_at", formatContextTime(e.CreatedAt))
		writeContextField(sb, "updated_at", formatContextTime(e.UpdatedAt))
		if len(e.Sources) > 0 {
			sb.WriteString("  sources:\n")
			for _, src := range e.Sources {
				sb.WriteString(fmt.Sprintf(
					"    - type: %s, kind: %s, ref: %s, at: %s\n",
					src.Type, src.Kind, src.Ref, formatContextTime(src.Timestamp)))
			}
		}
	}
}

func writeContextField(sb *strings.Builder, key, value string) {
	sb.WriteString("  ")
	sb.WriteString(key)
	sb.WriteString(": ")
	sb.WriteString(value)
	sb.WriteString("\n")
}

// contextDisplayOrder is the fixed order kinds appear in the compact output.
var contextDisplayOrder = []personalcontext.Kind{
	personalcontext.KindIdentity,
	personalcontext.KindPreference,
	personalcontext.KindFact,
	personalcontext.KindGoal,
	personalcontext.KindRelationship,
	personalcontext.KindRoutine,
	personalcontext.KindDecision,
	personalcontext.KindConsent,
}

// contextDisplayKinds is the human-readable section header per kind.
var contextDisplayKinds = map[personalcontext.Kind]string{
	personalcontext.KindIdentity:     "Identity",
	personalcontext.KindPreference:   "Preferences",
	personalcontext.KindFact:         "Facts",
	personalcontext.KindGoal:         "Goals",
	personalcontext.KindRelationship: "Relationships",
	personalcontext.KindRoutine:      "Routines",
	personalcontext.KindDecision:     "Decisions",
	personalcontext.KindConsent:      "Consent",
}

// shortPredicate returns the predicate without its kind prefix for display in
// a kind-grouped view ("identity/name" -> "name").
func shortPredicate(predicate string) string {
	if i := strings.LastIndex(predicate, "/"); i >= 0 {
		return predicate[i+1:]
	}
	return predicate
}

// renderContextValue renders an entry value compactly: strings appear unquoted
// with newlines collapsed; anything else is emitted as compact JSON.
func renderContextValue(e personalcontext.Entry) string {
	var s string
	if err := json.Unmarshal(e.Value, &s); err == nil {
		s = strings.ReplaceAll(s, "\r", " ")
		s = strings.ReplaceAll(s, "\n", " ")
		return strings.TrimSpace(s)
	}
	raw := string(e.Value)
	raw = strings.ReplaceAll(raw, "\r", " ")
	raw = strings.ReplaceAll(raw, "\n", " ")
	return strings.TrimSpace(raw)
}

func formatContextTimePtr(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return formatContextTime(*t)
}

func formatContextTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}
