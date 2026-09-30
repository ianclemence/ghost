package relaycrypto

import (
	"bytes"
	"testing"
)

func pair(t *testing.T) (phone, pod *Session, id *Identity) {
	t.Helper()
	id, err := NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	phone, eph, err := Dial(id.Public())
	if err != nil {
		t.Fatal(err)
	}
	pod, err = id.Accept(eph)
	if err != nil {
		t.Fatal(err)
	}
	return phone, pod, id
}

func TestMessagesRoundTripBothWays(t *testing.T) {
	phone, pod, _ := pair(t)
	for _, msg := range []string{"hello", "", "a much longer message " + string(make([]byte, 4000))} {
		s, _ := phone.Seal([]byte(msg))
		got, err := pod.Open(s)
		if err != nil || string(got) != msg {
			t.Fatalf("phone->pod failed: %v", err)
		}
		s, _ = pod.Seal([]byte(msg))
		got, err = phone.Open(s)
		if err != nil || string(got) != msg {
			t.Fatalf("pod->phone failed: %v", err)
		}
	}
}

func TestTheRelayCannotReadOrAlter(t *testing.T) {
	phone, pod, _ := pair(t)
	secret := []byte("the password is hunter2")
	s, _ := phone.Seal(secret)
	if bytes.Contains(s, []byte("hunter2")) {
		t.Fatal("plaintext visible in the sealed message")
	}
	for i := range s {
		bad := append([]byte{}, s...)
		bad[i] ^= 1
		if _, err := pod.Open(bad); err == nil {
			t.Fatalf("flipping byte %d was not detected", i)
		}
	}
	if _, err := pod.Open(s); err != nil {
		t.Fatalf("the untouched message should still open: %v", err)
	}
}

func TestReplayReorderAndReflectionAreRefused(t *testing.T) {
	phone, pod, _ := pair(t)
	a, _ := phone.Seal([]byte("one"))
	b, _ := phone.Seal([]byte("two"))
	if _, err := pod.Open(b); err != nil {
		t.Fatal(err)
	}
	if _, err := pod.Open(a); err != ErrReplay {
		t.Fatalf("an older message must be refused, got %v", err)
	}
	if _, err := pod.Open(b); err != ErrReplay {
		t.Fatalf("a repeated message must be refused, got %v", err)
	}
	// Reflecting the phone's own message back at it must fail: the directions
	// use different keys.
	if _, err := phone.Open(a); err == nil {
		t.Fatal("a message reflected back to its sender must not open")
	}
}

func TestAnImpostorKeyCannotListen(t *testing.T) {
	phone, _, _ := pair(t)
	other, _ := NewIdentity()
	s, _ := phone.Seal([]byte("secret"))
	// A relay standing in with its own key derives different keys.
	mitm, eph, _ := Dial(other.Public())
	_ = mitm
	pod2, _ := other.Accept(eph)
	if _, err := pod2.Open(s); err == nil {
		t.Fatal("a session with another key must not open this message")
	}
}

func TestSessionsAreIndependent(t *testing.T) {
	p1, _, id := pair(t)
	_, eph2, _ := Dial(id.Public())
	pod2, _ := id.Accept(eph2)
	s, _ := p1.Seal([]byte("x"))
	if _, err := pod2.Open(s); err == nil {
		t.Fatal("a message from one connection must not open on another")
	}
}

func TestBadInputIsRefusedNotTrusted(t *testing.T) {
	_, pod, id := pair(t)
	if _, err := pod.Open(nil); err != ErrShort {
		t.Fatal("empty must be refused")
	}
	if _, err := pod.Open(make([]byte, 40)); err == nil {
		t.Fatal("zero counter must be refused")
	}
	if _, _, err := Dial([]byte("short")); err != ErrBadKey {
		t.Fatal("a malformed pinned key must be refused")
	}
	if _, err := id.Accept(make([]byte, 32)); err == nil {
		t.Fatal("an all-zero public key must be refused")
	}
	back, err := LoadIdentity(id.Private())
	if err != nil || !bytes.Equal(back.Public(), id.Public()) {
		t.Fatal("an identity must survive storage")
	}
}
