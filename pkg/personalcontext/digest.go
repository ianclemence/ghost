package personalcontext

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"
)

// DigestBudget is the hard character budget for the Active Context Digest.
// The digest is a model-facing, bounded rendering of the current Personal
// Context: it never grows without bound no matter how large the store grows.
const DigestBudget = 600

// BuildDigest renders the Active Context Digest for a set of current entries.
// It is pure and deterministic: the same input produces the same bytes, with
// no LLM, no embeddings, and no dependence on map iteration order. Callers are
// expected to pass Store.Current()/CurrentAt() so the digest inherits the
// store's own semantics (status current plus temporal validity); superseded,
// rejected, conflicting, uncertain, expired, and future-valid entries never
// reach the digest because they never reach Current().
//
// Entries are prioritized (identity first, then preferences, goals,
// relationships, routines, and other durable facts), tied deterministically by
// predicate then id, and emitted until the budget is exhausted — highest
// priority first. The returned section is delimited with <personal_context>
// tags so stored values are treated as data, never as instructions.
func BuildDigest(current []Entry, budget int) string {
	if budget <= 0 {
		budget = DigestBudget
	}
	if len(current) == 0 {
		return ""
	}

	entries := append([]Entry(nil), current...)
	sort.SliceStable(entries, func(i, j int) bool {
		return digestLess(entries[i], entries[j])
	})

	// Reserve room for the delimiters up front so the emitted section always
	// fits the hard budget.
	room := budget - len(digestOpen) - len(digestClose)
	if room <= 0 {
		return ""
	}

	var sb strings.Builder
	for _, e := range entries {
		line := "- " + digestLabel(e.Predicate) + ": " + digestValue(e)
		if sb.Len()+len(line)+1 > room {
			if sb.Len() == 0 {
				// Even the top-priority entry alone cannot fit; truncate its
				// value so the digest is never empty and the cap always holds.
				// Reserve one byte for the trailing newline.
				if room > 1 {
					line = truncateDigestLine(line, room-1)
				} else {
					break
				}
			} else {
				// Budget exhausted; everything left is lower priority.
				break
			}
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	if sb.Len() == 0 {
		return ""
	}
	return digestOpen + sb.String() + digestClose
}

const (
	digestOpen  = "## Personal Context\n\n<personal_context>\n"
	digestClose = "</personal_context>"
)

// digestPriority ranks an entry's conversational value. Lower numbers are
// injected first. Preferences split into "important preferences" (everything
// except the communication style) and "communication preferences" (which the
// model needs at the very start of every turn).
func digestPriority(e Entry) int {
	switch e.Kind {
	case KindIdentity:
		return 1
	case KindPreference:
		if e.Predicate == "preference/communication.style" {
			return 3
		}
		return 2
	case KindGoal:
		return 4
	case KindRelationship:
		return 5
	case KindRoutine:
		return 6
	default: // fact, decision, consent, ...
		return 7
	}
}

// digestLess is the deterministic total order: priority, then predicate, then
// id. Two stores with the same entries yield byte-identical digests.
func digestLess(a, b Entry) bool {
	if pa, pb := digestPriority(a), digestPriority(b); pa != pb {
		return pa < pb
	}
	if a.Predicate != b.Predicate {
		return a.Predicate < b.Predicate
	}
	return a.ID < b.ID
}

// digestLabels maps known predicates to compact, human-readable labels.
// Unknown predicates fall back to the suffix after the last "/" with
// separators spelled out.
var digestLabels = map[string]string{
	"identity/name":                  "Name",
	"identity/age":                   "Age",
	"identity/gender":                "Gender",
	"fact/location":                  "Location",
	"fact/home":                      "Home",
	"fact/job":                       "Job",
	"fact/work":                      "Work",
	"fact/school":                    "School",
	"preference/favorite_color":      "Favorite color",
	"preference/favorite_food":       "Favorite food",
	"preference/favorite_music":      "Favorite music",
	"preference/communication.style": "Communication style",
	"preference/likes":               "Likes",
	"preference/prefers":             "Prefers",
	"preference/favorite":            "Favorite",
	"goal/primary":                   "Goal",
	"relationship/partner":           "Partner",
	"relationship/family":            "Family",
	"routine/sleep":                  "Sleep",
	"routine/exercise":               "Exercise",
}

func digestLabel(predicate string) string {
	if label, ok := digestLabels[predicate]; ok {
		return label
	}
	if i := strings.LastIndex(predicate, "/"); i >= 0 {
		predicate = predicate[i+1:]
	}
	return strings.TrimSpace(strings.NewReplacer(".", " ", "_", " ").Replace(predicate))
}

// Label returns a human-friendly label for a predicate, e.g. "identity/name" ->
// "Name" and "preference/favorite_color" -> "Favorite color". It is the
// surfaced, user-facing form used by the console's personal-context view.
func Label(predicate string) string {
	return digestLabel(predicate)
}

// Value renders an entry's value as readable plain text (unquoted for string
// values, collapsed single line otherwise). It is the user-facing form of an
// entry's value for the console.
func Value(e Entry) string {
	return digestValue(e)
}

// digestValue renders an entry's value compactly. String values appear
// unquoted ("Bangkok", not `"Bangkok"`); anything else is emitted as compact
// JSON. Newlines are collapsed so a stored value can never break the digest
// structure or masquerade as prompt content.
func digestValue(e Entry) string {
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

// truncateDigestLine cuts a line to at most max bytes so the hard budget
// always holds even when a single value is enormous. A trailing ellipsis rune
// marks the truncation without ever exceeding max.
func truncateDigestLine(line string, max int) string {
	if len(line) <= max {
		return line
	}
	if max <= 1 {
		return safeByteTruncate(line, max)
	}
	// The ellipsis is 3 bytes; reserve room for it.
	return safeByteTruncate(line, max-3) + "…"
}

// safeByteTruncate returns the longest prefix of s that is at most max bytes
// and never splits a multi-byte UTF-8 rune.
func safeByteTruncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}
