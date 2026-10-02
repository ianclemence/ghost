package personalcontext

import "sort"

// PendingChange is something Ghost heard that disagrees with something the
// owner told it. The old belief stays current; the new one waits, unconfirmed,
// until the owner says which is right.
type PendingChange struct {
	Current   Entry
	Candidate Entry
}

// PendingChanges lists unconfirmed candidates visible to the scopes whose
// subject and predicate match a current belief with a different value, newest
// first. A candidate with nothing to disagree with is not a pending change.
func (s *Store) PendingChanges(scopes []string) []PendingChange {
	cur := map[string]Entry{}
	for _, e := range s.CurrentInScope(scopes) {
		cur[e.Subject+"\x00"+e.Predicate] = e
	}
	var out []PendingChange
	for _, e := range s.All() {
		if e.Status != StatusUncertain || !VisibleTo(e, scopes) {
			continue
		}
		c, ok := cur[e.Subject+"\x00"+e.Predicate]
		if !ok || Value(c) == Value(e) {
			continue
		}
		out = append(out, PendingChange{Current: c, Candidate: e})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Candidate.CreatedAt.After(out[j].Candidate.CreatedAt) })
	return out
}
