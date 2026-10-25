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
func (s *Store) ForeignCurrent(isOwner func(session string) bool) []Entry {
	var out []Entry
	for _, e := range s.Current() {
		if len(e.Scopes) > 0 || isOwner(SessionOf(e)) {
			continue
		}
		out = append(out, e)
	}
	return out
}
