package memsync

import "testing"

func TestDedupAndOrdering(t *testing.T) {
	l := NewLog()
	a := Op{OpID: "a", Origin: "phone", EntityID: "e1", EntityKind: "fact", EntityVers: 1, Scope: ScopeSharedDura, Type: OpUpsert, OriginClock: 1}
	if ok, err := l.Apply(a); err != nil || !ok {
		t.Fatalf("first apply must change state: %v %v", ok, err)
	}
	if ok, _ := l.Apply(a); ok {
		t.Fatal("duplicate must collapse")
	}
	stale := a
	stale.OpID = "b"
	stale.EntityVers = 1
	stale.OriginClock = 2
	// same version upsert after winner: no change (not a delete tie-break)
	if ok, _ := l.Apply(stale); ok {
		t.Fatal("stale same-version upsert must not win")
	}
	del := Op{OpID: "c", Origin: "pod", EntityID: "e1", EntityKind: "fact", EntityVers: 1, Scope: ScopeSharedDura, Type: OpDelete, OriginClock: 3}
	if ok, _ := l.Apply(del); !ok {
		t.Fatal("same-version tombstone must win ties")
	}
	v2 := Op{OpID: "d", Origin: "pod", EntityID: "e1", EntityKind: "fact", EntityVers: 2, Scope: ScopeSharedDura, Type: OpUpsert, OriginClock: 4}
	if ok, _ := l.Apply(v2); !ok {
		t.Fatal("newer version must resurrect over tombstone")
	}
	if got := l.Since(0); len(got) == 0 {
		t.Fatal("since must replay")
	}
}

func TestValidation(t *testing.T) {
	l := NewLog()
	if _, err := l.Apply(Op{}); err == nil {
		t.Fatal("want validation error")
	}
}
