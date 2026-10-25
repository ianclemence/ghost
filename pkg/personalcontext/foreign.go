package personalcontext

import "strings"

// SessionOf names the conversation an entry was learned in: the part of its
// first source's ref before the first colon ("main:req-17..." is "main",
// "memtest:req-17..." is "memtest"). Entries from before sources carried a ref
// have none.
func SessionOf(e Entry) string {
	if len(e.Sources) == 0 {
		return ""
	}
	ref := e.Sources[0].Ref
	if i := strings.Index(ref, ":"); i >= 0 {
		ref = ref[:i]
	}
	return ref
}

// ForeignCurrent lists the current, global beliefs that were learned in a
// conversation that is not the owner's. Test clients and probes reach the same
// gateway as the owner under keys of their own, and before the runtime stopped
// learning from them their text was filed here as facts about the owner. An
// entry in a scoped context is the owner's by construction and is never listed.
//
// A belief the owner's own conversation also established is never listed, even
// when its only surviving copy came from a probe: it is true, and forgetting it
// would lose something the owner said. Matching is on the text, case and spacing
// aside.
func (s *Store) ForeignCurrent(isOwner func(session string) bool) []Entry {
	said := map[string]bool{}
	for _, e := range s.All() {
		if isOwner(SessionOf(e)) && string(e.Value) != string(erasedValue) {
			said[normalizeBelief(string(e.Value))] = true
		}
	}
	var out []Entry
	for _, e := range s.Current() {
		if len(e.Scopes) > 0 || isOwner(SessionOf(e)) || said[normalizeBelief(string(e.Value))] {
			continue
		}
		out = append(out, e)
	}
	return out
}

func normalizeBelief(raw string) string {
	return strings.Join(strings.Fields(strings.ToLower(raw)), " ")
}
