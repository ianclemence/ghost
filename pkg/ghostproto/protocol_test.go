package ghostproto

import "testing"

func TestVersionNegotiation(t *testing.T) {
	e := Envelope{ProtoVersion: Version, Type: TypeTask, MessageID: "m1"}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := Envelope{ProtoVersion: 9999, Type: TypeTask, MessageID: "m1"}
	if err := bad.Validate(); err == nil {
		t.Fatal("want version mismatch error")
	}
}

func TestReplayProtection(t *testing.T) {
	n := NewNonceWindow(0)
	if !n.Check("n1", 1000) {
		t.Fatal("first use must pass")
	}
	if n.Check("n1", 1001) {
		t.Fatal("replay must be rejected")
	}
}
