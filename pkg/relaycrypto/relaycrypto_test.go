package relaycrypto

import (
	"bytes"
	"testing"
	"time"
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

func TestSealedExchangeRoundTrip(t *testing.T) {
	pod, _ := NewIdentity()
	phoneSess, env, err := SealRequest(pod.Public(), []byte(`{"path":"/v1/chat"}`))
	if err != nil {
		t.Fatal(err)
	}
	podSess, req, eph, err := pod.OpenRequest(env)
	if err != nil || string(req) != `{"path":"/v1/chat"}` || len(eph) != KeySize {
		t.Fatalf("open request: %q %v", req, err)
	}
	var wire []byte
	for _, m := range []struct {
		kind byte
		body string
	}{{MsgHead, "head"}, {MsgData, "hello "}, {MsgData, "world"}, {MsgEnd, ""}} {
		s, err := podSess.SealMessage(m.kind, []byte(m.body))
		if err != nil {
			t.Fatal(err)
		}
		wire = append(wire, Frame(s)...)
	}
	// Deliver in awkward pieces, the way a network does.
	var got []string
	var rest []byte
	for i := 0; i < len(wire); i += 7 {
		end := i + 7
		if end > len(wire) {
			end = len(wire)
		}
		var frames [][]byte
		frames, rest = SplitFrames(append(rest, wire[i:end]...))
		for _, f := range frames {
			kind, body, err := phoneSess.OpenMessage(f)
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, string([]byte{'0' + kind})+string(body))
		}
	}
	want := []string{"0head", "1hello ", "1world", "2"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestOpenRequestRejectsWrongKeyAndTampering(t *testing.T) {
	pod, _ := NewIdentity()
	other, _ := NewIdentity()
	_, env, _ := SealRequest(pod.Public(), []byte("secret"))
	if _, _, _, err := other.OpenRequest(env); err == nil {
		t.Fatal("a different Pod must not open the request")
	}
	bad := append([]byte(nil), env...)
	bad[len(bad)-1] ^= 1
	if _, _, _, err := pod.OpenRequest(bad); err == nil {
		t.Fatal("a tampered request must not open")
	}
	if _, _, _, err := pod.OpenRequest([]byte{9, 1, 2}); err == nil {
		t.Fatal("junk must not open")
	}
}

func TestReplayGuard(t *testing.T) {
	g := NewReplayGuard(time.Minute)
	now := time.Now()
	if !g.Fresh([]byte("k1"), now) {
		t.Fatal("first sight is fresh")
	}
	if g.Fresh([]byte("k1"), now.Add(time.Second)) {
		t.Fatal("a repeat inside the window is a replay")
	}
	if !g.Fresh([]byte("k1"), now.Add(3*time.Minute)) {
		t.Fatal("after the memory window passes the key is forgotten")
	}
}
