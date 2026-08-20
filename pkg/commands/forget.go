package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/session"
)

// forgetHandler implements /forget: the user-facing way to retire Personal
// Context entries. It is deterministic, never calls an LLM, never searches
// conversations, and never touches RAG or MEMORY.md.
//
// A normal /forget only retires entries in the personalcontext.Store (each
// gets a rejected revision); it never deletes conversation evidence. Deleting
// conversation evidence requires the explicit /forget session <id> form, which
// removes the session transcript through the session storage API and retires
// any Personal Context entries whose provenance references that session.
//
// Syntax:
//
//	/forget <predicate>                 e.g. preference/favorite_color
//	/forget <suffix>                    e.g. favorite_color
//	/forget <topic>                     e.g. my favorite color, location
//	/forget everything about <topic>    e.g. everything about my relationship with Jane
//	/forget session <session-id>        delete conversation evidence for a session
//
// Resolution favours false negatives over destructive false positives: an
// ambiguous target (multiple distinct predicates match) is never deleted and
// the matching predicates are listed so the user can narrow the request.
func forgetHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.PersonalContext == nil {
		return req.Reply("Personal Context is unavailable.")
	}

	rest := forgetRest(req.Text)
	if rest == "" {
		return req.Reply(forgetUsage)
	}

	fields := strings.Fields(rest)
	switch fields[0] {
	case "session":
		return forgetSession(req, rt, rest)
	case "everything":
		if len(fields) < 3 || fields[1] != "about" {
			return req.Reply("Refusing: /forget everything would delete too much. Use `/forget everything about <topic>` to scope it.")
		}
		return forgetEverythingAbout(req, rt, strings.Join(fields[2:], " "))
	default:
		return forgetTarget(req, rt, rest)
	}
}

const forgetUsage = "Usage: /forget <predicate | suffix | topic> | /forget everything about <topic> | /forget session <session-id>"

// forgetRest returns the command text after the leading "/forget".
func forgetRest(text string) string {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return ""
	}
	return strings.Join(fields[1:], " ")
}

// forgetTarget retires the entries matching a single target phrase. It is the
// normal, non-destructive path: nothing is ever deleted, and an ambiguous
// match is refused in favour of asking the user to narrow the request.
func forgetTarget(req Request, rt *Runtime, phrase string) error {
	store := rt.PersonalContext
	matches := forgetMatches(activeForgetEntries(store, time.Now()), phrase)

	if len(matches) == 0 {
		// Distinguish "already forgotten" from "never existed / not current"
		// so a repeated /forget reports the truth instead of a placeholder.
		all := forgetMatches(store.All(), phrase)
		if len(all) > 0 {
			allRejected := true
			for _, e := range all {
				if e.Status != personalcontext.StatusRejected {
					allRejected = false
					break
				}
			}
			if allRejected {
				return req.Reply("That Personal Context is already forgotten.")
			}
		}
		return req.Reply(fmt.Sprintf("No current Personal Context entry matches %q.", phrase))
	}

	// Distinct beliefs (subject + predicate) are never resolved silently.
	beliefs := forgetBeliefs(matches)
	if len(beliefs) > 1 {
		return req.Reply(renderForgetAmbiguous(phrase, beliefs))
	}
	// A single belief group that holds parallel current entries (e.g. two
	// relationship/partner values for different people) is ambiguous too —
	// only one contested (conflicting/uncertain) belief may be retired whole.
	group := beliefs[0]
	if len(group) > 1 {
		allUnresolved := true
		for _, e := range group {
			if !forgetUnresolved(e) {
				allUnresolved = false
				break
			}
		}
		if !allUnresolved {
			return req.Reply(renderForgetAmbiguous(phrase, beliefs))
		}
	}

	for _, e := range group {
		if err := store.Forget(e.ID); err != nil {
			return req.Reply(fmt.Sprintf("Failed to forget %s: %v", e.Predicate, err))
		}
	}
	if len(group) == 1 {
		return req.Reply(fmt.Sprintf("Forgotten: %s", group[0].Predicate))
	}
	return req.Reply(fmt.Sprintf("Forgotten %d Personal Context entries for %s.", len(group), group[0].Predicate))
}

// forgetBeliefs groups matching entries by subject + predicate, preserving
// match order. Each belief is a single contested piece of context: a conflict
// pair is one belief, while two same-predicate entries with different values
// are two beliefs.
func forgetBeliefs(matches []personalcontext.Entry) [][]personalcontext.Entry {
	var beliefs [][]personalcontext.Entry
	index := make(map[string]int)
	for _, e := range matches {
		key := e.Subject + "\x00" + e.Predicate
		if i, ok := index[key]; ok {
			beliefs[i] = append(beliefs[i], e)
			continue
		}
		index[key] = len(beliefs)
		beliefs = append(beliefs, []personalcontext.Entry{e})
	}
	return beliefs
}

func forgetUnresolved(e personalcontext.Entry) bool {
	return e.Status == personalcontext.StatusConflicting || e.Status == personalcontext.StatusUncertain
}

// renderForgetAmbiguous lists the distinct beliefs so the user can narrow the
// request. When a belief holds several entries (parallel values for the same
// predicate), each value is shown so the listing stays unambiguous.
func renderForgetAmbiguous(phrase string, beliefs [][]personalcontext.Entry) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Multiple Personal Context entries match %q:\n", phrase))
	for _, group := range beliefs {
		if len(group) > 1 {
			for _, e := range group {
				sb.WriteString(fmt.Sprintf("- %s = %s\n", group[0].Predicate, forgetStringValue(e)))
			}
			continue
		}
		sb.WriteString(fmt.Sprintf("- %s\n", group[0].Predicate))
	}
	sb.WriteString("Please specify which one you want to forget, or use `/forget everything about <topic>`.")
	return strings.TrimRight(sb.String(), "\n")
}

// forgetEverythingAbout retires every active entry that clearly belongs to a
// scoped topic. Only structured, unambiguous matches are acted on: an exact
// subject, a relationship partner named after "relationship with/to", or a
// predicate whose suffix contains the canonicalized topic. Generic or
// self-referential topics are refused rather than interpreted destructively.
func forgetEverythingAbout(req Request, rt *Runtime, rawTopic string) error {
	store := rt.PersonalContext
	topic := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rawTopic), "my ")))
	if topic == "" || forgetGenericTopic(topic) {
		return req.Reply(fmt.Sprintf("Refusing: %q is too broad to retire safely. Name a specific topic.", rawTopic))
	}

	var targets []personalcontext.Entry
	active := activeForgetEntries(store, time.Now())

	// Relationship form: "everything about my relationship with X" names a
	// partner; match relationship entries whose value is that partner.
	if partner, ok := forgetRelationshipPartner(topic); ok {
		for _, e := range active {
			if e.Kind == personalcontext.KindRelationship &&
				strings.ToLower(forgetStringValue(e)) == partner {
				targets = append(targets, e)
			}
		}
	} else {
		canonical := canonicalizeForget(topic)
		for _, e := range active {
			if strings.ToLower(e.Subject) == topic {
				targets = append(targets, e)
				continue
			}
			if canonical != "" && len(canonical) >= 3 && strings.Contains(canonicalizeForget(shortPredicate(e.Predicate)), canonical) {
				targets = append(targets, e)
			}
		}
	}

	if len(targets) == 0 {
		return req.Reply(fmt.Sprintf("No current Personal Context entry mentions %q.", rawTopic))
	}
	for _, e := range targets {
		if err := store.Forget(e.ID); err != nil {
			return req.Reply(fmt.Sprintf("Failed to forget %s: %v", e.Predicate, err))
		}
	}
	if len(targets) == 1 {
		return req.Reply(fmt.Sprintf("Forgotten 1 Personal Context entry related to %q.", rawTopic))
	}
	return req.Reply(fmt.Sprintf("Forgotten %d Personal Context entries related to %q.", len(targets), rawTopic))
}

// forgetSession is the explicit "delete evidence" operation: it requires the
// session to exist, deletes the conversation evidence through the session
// storage API, and retires any active Personal Context entries whose
// provenance references the session. Unrelated entries and other sessions are
// left untouched.
func forgetSession(req Request, rt *Runtime, rest string) error {
	if rt.Sessions == nil {
		return req.Reply("Session manager unavailable.")
	}
	id := strings.TrimSpace(strings.TrimPrefix(rest, "session"))
	if id == "" {
		return req.Reply("Usage: /forget session <session-id>")
	}

	if !forgetSessionExists(rt.Sessions, id) {
		return req.Reply(fmt.Sprintf("No session found with id %q.", id))
	}

	var dependent []personalcontext.Entry
	for _, e := range activeForgetEntries(rt.PersonalContext, time.Now()) {
		if forgetSessionInProvenance(e, id) {
			dependent = append(dependent, e)
		}
	}

	if err := rt.Sessions.DeleteSession(id); err != nil {
		return req.Reply(fmt.Sprintf("Failed to delete session %q: %v", id, err))
	}
	for _, e := range dependent {
		if err := rt.PersonalContext.Forget(e.ID); err != nil {
			return req.Reply(fmt.Sprintf("Failed to forget %s: %v", e.Predicate, err))
		}
	}

	if len(dependent) == 0 {
		return req.Reply(fmt.Sprintf("Deleted session %q.", id))
	}
	if len(dependent) == 1 {
		return req.Reply(fmt.Sprintf("Deleted session %q and retired 1 dependent Personal Context entry.", id))
	}
	return req.Reply(fmt.Sprintf("Deleted session %q and retired %d dependent Personal Context entries.", id, len(dependent)))
}

// activeForgetEntries returns the entries /forget may act on: current and
// temporally valid entries (reusing the store's CurrentAt semantics, so
// expired and future-valid entries are never touched) plus the unresolved
// conflicting/uncertain set that /context surfaces but CurrentAt excludes.
func activeForgetEntries(store *personalcontext.Store, now time.Time) []personalcontext.Entry {
	var out []personalcontext.Entry
	seen := make(map[string]bool)
	for _, e := range store.CurrentAt(now) {
		out = append(out, e)
		seen[e.ID] = true
	}
	for _, e := range store.All() {
		if e.Status == personalcontext.StatusConflicting || e.Status == personalcontext.StatusUncertain {
			if !seen[e.ID] {
				out = append(out, e)
				seen[e.ID] = true
			}
		}
	}
	return out
}

// forgetMatches returns the entries whose predicate, predicate suffix, kind,
// or a canonicalized form of the phrase matches. The phrase is interpreted
// conservatively: exact predicate and suffix matches always win, kind matches
// apply only when the phrase is a known kind, and substring matching is
// restricted to reasonably long canonical forms to avoid trivial over-matches.
func forgetMatches(entries []personalcontext.Entry, phrase string) []personalcontext.Entry {
	canonical := canonicalizeForget(phrase)
	if canonical == "" {
		return nil
	}
	var out []personalcontext.Entry
	for _, e := range entries {
		if forgetEntryMatches(e, canonical) {
			out = append(out, e)
		}
	}
	return out
}

func forgetEntryMatches(e personalcontext.Entry, canonical string) bool {
	if canonical == e.Predicate {
		return true
	}
	suffix := shortPredicate(e.Predicate)
	suffixToken := canonicalizeForget(suffix)
	if canonical == suffix || canonical == suffixToken {
		return true
	}
	if personalcontext.ValidKind(personalcontext.Kind(canonical)) && string(e.Kind) == canonical {
		return true
	}
	if len(canonical) >= 3 &&
		(strings.Contains(suffixToken, canonical) || strings.Contains(canonical, suffixToken)) {
		return true
	}
	return false
}

// canonicalizeForget normalizes a user phrase for matching: lowercase, leading
// "my " stripped, and word separators collapsed to a single underscore. It
// returns "" for phrases that carry no usable matching signal.
func canonicalizeForget(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "my ")
	if s == "" || s == "my" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case ' ', '-', '.', '/':
			b.WriteByte('_')
		case '!', '?', ',', '\'', '"':
			// drop punctuation
		default:
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "_")
}

// forgetGenericTopic reports whether a topic is too broad to retire safely.
func forgetGenericTopic(topic string) bool {
	switch topic {
	case "me", "myself", "i", "my", "user", "us", "we", "our", "you", "everything", "personal", "context":
		return true
	}
	return false
}

// forgetRelationshipPartner extracts the partner name from a topic that starts
// with "relationship with" or "relationship to", returning ok=false otherwise.
func forgetRelationshipPartner(topic string) (string, bool) {
	for _, prefix := range []string{"relationship with ", "relationship to "} {
		if strings.HasPrefix(topic, prefix) {
			partner := strings.TrimSpace(strings.TrimPrefix(topic, prefix))
			if partner != "" && !forgetGenericTopic(partner) {
				return partner, true
			}
		}
	}
	return "", false
}

// forgetStringValue renders an entry's value as a plain string when it is a
// JSON string, and "" otherwise. Structured values are never guessed at.
func forgetStringValue(e personalcontext.Entry) string {
	var s string
	if err := json.Unmarshal(e.Value, &s); err == nil {
		return strings.TrimSpace(s)
	}
	return ""
}

// forgetSessionExists reports whether a session carries any evidence (a
// transcript or a summary).
func forgetSessionExists(sm *session.SessionManager, key string) bool {
	return len(sm.GetHistory(key)) > 0 || sm.GetSummary(key) != ""
}

// forgetSessionInProvenance reports whether any of an entry's sources
// references the given session (provenance refs are "session_key:message_id").
func forgetSessionInProvenance(e personalcontext.Entry, sessionKey string) bool {
	prefix := sessionKey + ":"
	for _, src := range e.Sources {
		if strings.HasPrefix(src.Ref, prefix) {
			return true
		}
	}
	return false
}
