package watch

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Rendering turns observed state into the owner's words. Every string here
// is owner-facing; every claim in it comes from a stored observation. The
// templates never speculate: no "probably", no invented times, no source
// that did not answer.

// SubjectPhrase is how a watch is named to its owner.
func SubjectPhrase(w Watch) string {
	label := strings.TrimSpace(w.Label)
	if label == "" {
		label = strings.TrimSpace(w.Entity)
	}
	switch w.Kind {
	case KindFlight:
		if !strings.Contains(strings.ToLower(label), "flight") {
			return "Flight " + strings.ToUpper(strings.TrimSpace(w.Entity))
		}
	case KindAppointment, KindReservation, KindDelivery, KindEvent:
		if label != "" && !strings.HasPrefix(strings.ToLower(label), "your ") {
			return "Your " + strings.ToLower(label)
		}
	}
	if label == "" {
		return "That watch"
	}
	return label
}

// fieldNames maps state keys to the words the owner uses.
var fieldNames = map[string]string{
	"gate":       "gate",
	"terminal":   "terminal",
	"status":     "status",
	"delay_min":  "delay",
	"scheduled":  "departure time",
	"number":     "flight number",
	"airline":    "airline",
	"from":       "origin",
	"to":         "destination",
	"time":       "time",
	"date":       "date",
	"location":   "location",
	"state":      "state",
	"window":     "delivery window",
	"room":       "room",
	"table":      "table",
	"platform":   "platform",
	"cancelled":  "cancellation",
	"track_link": "tracking link",
}

// FieldPhrase renders one state field for the owner.
func FieldPhrase(kind Kind, field string) string {
	if n, ok := fieldNames[field]; ok {
		return n
	}
	return strings.ReplaceAll(field, "_", " ")
}

// valuePhrase renders a value honestly: empty means "no longer set", which
// is a real observation worth saying out loud.
func valuePhrase(v string) string {
	if strings.TrimSpace(v) == "" {
		return "nothing set"
	}
	return v
}

// RenderNotice builds the owner-facing sentence for one detected change.
// It cites what changed and when it was observed — the evidence the notice
// rests on — without embellishment.
func RenderNotice(w Watch, changes []Change) string {
	if len(changes) == 0 {
		return ""
	}
	if w.Kind == KindPage && w.Rule != nil {
		return RenderPageNotice(w, changes)
	}
	subject := SubjectPhrase(w)
	parts := make([]string, 0, len(changes))
	for _, c := range changes {
		parts = append(parts, fmt.Sprintf("%s %s → %s",
			FieldPhrase(w.Kind, c.Field), valuePhrase(c.From), valuePhrase(c.To)))
	}
	msg := fmt.Sprintf("%s changed: %s.", subject, strings.Join(parts, "; "))
	if ev, ok := w.LastEvidence(); ok {
		observed := ev.At.In(time.Local).Format("15:04")
		if src := strings.TrimSpace(ev.Source); src != "" && src != "sandbox" {
			msg += fmt.Sprintf(" Observed at %s from %s.", observed, src)
		} else {
			msg += fmt.Sprintf(" Observed at %s.", observed)
		}
	}
	return msg
}

// RenderFailure is the honest sentence for a watch that has exhausted its
// retries: it says the watching stopped and why, and offers the one thing
// that would restart it.
func RenderFailure(w Watch) string {
	msg := fmt.Sprintf("I couldn't check %s, so I've stopped watching it for now.", SubjectPhrase(w))
	if reason := strings.TrimSpace(w.LastFailure); reason != "" {
		msg += fmt.Sprintf(" Last error: %s.", truncate(reason, 160))
	}
	return msg
}

// RenderExpired tells the owner a watch they asked for by name has run out
// of horizon. Automatic watches expire silently — nobody asked for those,
// and an unrequested message about a watch they never wanted is noise.
func RenderExpired(w Watch) string {
	if w.Kind == KindPage {
		return fmt.Sprintf("I've been watching %s for a month and nothing you asked for happened, so I've stopped. Ask again if you still want me to keep an eye on it.", SubjectPhrase(w))
	}
	return fmt.Sprintf("Your watch on %s has ended — the %s is past now.",
		SubjectPhrase(w), strings.ToLower(string(w.Kind)))
}

// RenderList renders the owner's watchlist for a state query.
func RenderList(list []Watch, now time.Time) string {
	if len(list) == 0 {
		return RenderNone()
	}
	lines := make([]string, 0, len(list))
	for _, w := range list {
		lines = append(lines, "- "+RenderSummary(w, now))
	}
	return fmt.Sprintf("You have %d %s:\n%s", len(list), plural(len(list), "watch", "watches"), strings.Join(lines, "\n"))
}

// RenderSummary is one line of watchlist.
func RenderSummary(w Watch, now time.Time) string {
	state := string(w.Status)
	switch w.Status {
	case StatusActive, StatusTriggered:
		if w.NextCheck != nil {
			state = "active, next check " + relative(w.NextCheck.Sub(now))
		} else {
			state = "active"
		}
	case StatusSnoozed:
		if w.SnoozeUntil != nil {
			state = "snoozed until " + w.SnoozeUntil.In(time.Local).Format("Mon 15:04")
		} else {
			state = "snoozed"
		}
	case StatusFailed:
		state = "couldn't be checked"
	case StatusDisabled:
		state = "stopped"
	case StatusExpired:
		state = "expired"
	case StatusCompleted:
		state = "finished"
	}
	out := fmt.Sprintf("%s (%s)", SubjectPhrase(w), state)
	if len(w.Notified) > 0 {
		out += fmt.Sprintf(", %d %s", len(w.Notified), plural(len(w.Notified), "change", "changes"))
	}
	return out
}

// RenderNone is the empty watchlist answer.
func RenderNone() string {
	return "You're not watching anything right now."
}

// ReasonText turns a policy verdict into the honest owner-facing refusal.
// It always states what is missing rather than a generic failure.
func ReasonText(v Verdict) string {
	switch v.Reason {
	case ReasonSourceUnavailable:
		return "I can't watch that yet — there's no source connected that could check it for me."
	case ReasonProactiveDisabled:
		return "Automatic watching is turned off in your proactive preferences."
	case ReasonHorizon:
		return "That's too far out for me to watch usefully yet."
	case ReasonPastEvent:
		return "That already happened, so there's nothing to watch."
	case ReasonDuplicate:
		return "I'm already watching that."
	case ReasonMaxActive:
		return "I'm already watching as many things as I can keep up with right now."
	case ReasonNoEntity:
		return "I couldn't tell what to watch."
	case ReasonMissingProvenance:
		return "I need you to tell me what to watch."
	default:
		return "I couldn't set that watch up."
	}
}

// LastEvidence returns the most recent observation backing this watch.
func (w Watch) LastEvidence() (Evidence, bool) {
	if len(w.Evidence) == 0 {
		return Evidence{}, false
	}
	return w.Evidence[len(w.Evidence)-1], true
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// relative renders a duration the way a person would say it.
func relative(d time.Duration) string {
	if d < 0 {
		return "now"
	}
	switch {
	case d < time.Minute:
		return "in a moment"
	case d < time.Hour:
		return fmt.Sprintf("in %dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("in %dh", int(d.Hours()))
	default:
		return fmt.Sprintf("in %dd", int(d.Hours()/24))
	}
}

// SortedStates renders a state map deterministically (diagnostics, tests).
func SortedStates(state map[string]string) []string {
	keys := make([]string, 0, len(state))
	for k := range state {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+state[k])
	}
	return out
}
