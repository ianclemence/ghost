package personalcontext

import (
	"encoding/json"
	"testing"
	"time"
)

func entryWith(id, predicate, val string, conf float64, reinforced int, up time.Time) Entry {
	v, _ := json.Marshal(val)
	return Entry{
		ID:             id,
		Kind:           KindPreference,
		Subject:        entrySubjectUser,
		Predicate:      predicate,
		Value:          v,
		Status:         StatusCurrent,
		Confidence:     conf,
		ReinforceCount: reinforced,
		CreatedAt:      up,
		UpdatedAt:      up,
	}
}

func TestImportanceRanksHigherConfidence(t *testing.T) {
	now := time.Now().UTC()
	hi := entryWith("a", "preference/prefers", "a", 0.95, 0, now)
	lo := entryWith("b", "preference/prefers", "b", 0.4, 0, now)
	if Importance(hi, now) <= Importance(lo, now) {
		t.Fatalf("higher confidence should score higher")
	}
}

func TestImportanceRanksReinforcement(t *testing.T) {
	now := time.Now().UTC()
	reinforced := entryWith("a", "preference/prefers", "a", 0.7, 5, now)
	reinforced.ReinforcedAt = &now
	plain := entryWith("b", "preference/prefers", "b", 0.7, 0, now)
	if Importance(reinforced, now) <= Importance(plain, now) {
		t.Fatalf("reinforced entry should score higher")
	}
}

func TestImportanceDecaysWithAge(t *testing.T) {
	now := time.Now().UTC()
	recent := entryWith("a", "preference/prefers", "a", 0.7, 0, now)
	old := entryWith("b", "preference/prefers", "b", 0.7, 0, now.Add(-90*24*time.Hour))
	if Importance(recent, now) <= Importance(old, now) {
		t.Fatalf("recent entry should score higher than an old one")
	}
}

func TestDigestOrdersByImportanceWithinKind(t *testing.T) {
	now := time.Now().UTC()
	// Same kind/predicate: high confidence must be injected before low confidence.
	curr := []Entry{
		entryWith("a", "preference/prefers", "plain", 0.4, 0, now),
		entryWith("b", "preference/prefers", "reinforced", 0.95, 3, now),
	}
	d := BuildDigest(curr, 600)
	if !containedInOrder(d, "reinforced", "plain") {
		t.Fatalf("expected high-importance entry 'b' before low 'a', got:\n%s", d)
	}
}

func containedInOrder(s, first, second string) bool {
	i := indexOf(s, first)
	j := indexOf(s, second)
	return i >= 0 && j >= 0 && i < j
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
