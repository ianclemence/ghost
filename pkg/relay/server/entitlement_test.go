package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ianclemence/ghost/pkg/entitlement"
	"github.com/ianclemence/ghost/pkg/relay/proto"
	"github.com/ianclemence/ghost/pkg/relaycrypto"
)

type meteredEnv struct {
	srv  *Server
	ts   *httptest.Server
	priv ed25519.PrivateKey
	now  func() time.Time
	mu   *sync.Mutex
	t0   *time.Time
}

func newMetered(t *testing.T) *meteredEnv {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	var mu sync.Mutex
	t0 := time.Now()
	now := func() time.Time { mu.Lock(); defer mu.Unlock(); return t0 }
	srv, err := NewServer(Config{
		RegistryPath:    tempRegistry(t),
		EntitlementKeys: []ed25519.PublicKey{pub},
		Now:             now,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(srv.HandleHTTP))
	t.Cleanup(ts.Close)
	return &meteredEnv{srv: srv, ts: ts, priv: priv, now: now, mu: &mu, t0: &t0}
}

func (e *meteredEnv) advance(d time.Duration) { e.mu.Lock(); *e.t0 = e.t0.Add(d); e.mu.Unlock() }

func (e *meteredEnv) token(t *testing.T, pod, acct string, ttl time.Duration) string {
	t.Helper()
	n := e.now()
	tok, err := entitlement.Sign(e.priv, entitlement.Claims{Pod: pod, Account: acct, IssuedAt: n.Unix(), ExpiresAt: n.Add(ttl).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func (e *meteredEnv) enroll(pod, secret, tok string) *http.Response {
	body, _ := json.Marshal(map[string]string{"device_id": pod, "device_secret": secret, "entitlement": tok, "name": "t"})
	resp, err := http.Post(e.ts.URL+"/v1/enroll", "application/json", bytes.NewReader(body))
	if err != nil {
		panic(err)
	}
	return resp
}

func (e *meteredEnv) dial(pod, secret, tok string) (*websocket.Conn, *http.Response, error) {
	h := http.Header{"X-Ghost-Device": {pod}, "X-Ghost-Device-Secret": {secret}}
	if tok != "" {
		h.Set("X-Ghost-Entitlement", tok)
	}
	return websocket.DefaultDialer.Dial(wsURL(e.ts.URL)+"/v1/tunnel", h)
}

func TestEnrollWithEntitlement(t *testing.T) {
	e := newMetered(t)
	if r := e.enroll("pod-1", "s1", e.token(t, "pod-1", "acct-A", time.Hour)); r.StatusCode != 200 {
		t.Fatalf("a valid entitlement should enroll, got %d", r.StatusCode)
	}
	if !e.srv.registry.Authenticate("pod-1", "s1") {
		t.Fatal("enrolled device should authenticate")
	}
}

func TestEnrollRefusals(t *testing.T) {
	e := newMetered(t)
	cases := map[string]string{
		"wrong pod":      e.token(t, "other-pod", "acct-A", time.Hour),
		"expired":        e.token(t, "pod-1", "acct-A", -time.Hour),
		"no token":       "",
		"not a token":    "ge1.junk.junk",
		"someone else's": strings.Replace(e.token(t, "pod-1", "acct-A", time.Hour), "ge1.", "ge1.x", 1),
	}
	for name, tok := range cases {
		if r := e.enroll("pod-1", "s1", tok); r.StatusCode != 401 {
			t.Errorf("%s: got %d, want 401", name, r.StatusCode)
		}
	}
	if e.srv.registry.Authenticate("pod-1", "s1") {
		t.Fatal("nothing above should have enrolled the device")
	}
}

func TestReenrollOnlyByTheSameAccount(t *testing.T) {
	e := newMetered(t)
	e.enroll("pod-1", "s1", e.token(t, "pod-1", "acct-A", time.Hour))
	// Another subscriber, validly signed, still cannot take the device id over.
	if r := e.enroll("pod-1", "evil", e.token(t, "pod-1", "acct-B", time.Hour)); r.StatusCode != 409 {
		t.Fatalf("a different account must not re-enroll, got %d", r.StatusCode)
	}
	if e.srv.registry.Authenticate("pod-1", "evil") || !e.srv.registry.Authenticate("pod-1", "s1") {
		t.Fatal("the original secret must be untouched")
	}
	// The same account can, for example after a reinstall.
	if r := e.enroll("pod-1", "s2", e.token(t, "pod-1", "acct-A", time.Hour)); r.StatusCode != 200 {
		t.Fatalf("the same account should re-enroll, got %d", r.StatusCode)
	}
	if !e.srv.registry.Authenticate("pod-1", "s2") {
		t.Fatal("the new secret should work")
	}
}

func TestTunnelNeedsEntitlementOnMeteredRelay(t *testing.T) {
	e := newMetered(t)
	e.enroll("pod-1", "s1", e.token(t, "pod-1", "acct-A", time.Hour))

	if _, resp, err := e.dial("pod-1", "s1", ""); err == nil || resp == nil || resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("no entitlement should be 402, got %v %v", resp, err)
	}
	if _, resp, err := e.dial("pod-1", "s1", e.token(t, "pod-1", "acct-A", -time.Minute)); err == nil || resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("an expired entitlement should be 402, got %v", resp)
	}
	if _, resp, err := e.dial("pod-1", "s1", e.token(t, "pod-2", "acct-A", time.Hour)); err == nil || resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("another Pod's entitlement should be 402, got %v", resp)
	}
	conn, _, err := e.dial("pod-1", "s1", e.token(t, "pod-1", "acct-A", time.Hour))
	if err != nil {
		t.Fatalf("a current entitlement should connect: %v", err)
	}
	conn.Close()
}

func TestSelfHostedRelayIgnoresEntitlement(t *testing.T) {
	srv, _ := NewServer(Config{RegistryPath: tempRegistry(t)})
	secret, _ := srv.registry.Add("pod-1", "x")
	ts := httptest.NewServer(http.HandlerFunc(srv.HandleHTTP))
	defer ts.Close()
	conn := wsDial(t, ts, "pod-1", secret)
	conn.Close()
}

func TestTunnelClosesWhenEntitlementEnds(t *testing.T) {
	old := entitlementCheckEvery
	entitlementCheckEvery = 20 * time.Millisecond
	defer func() { entitlementCheckEvery = old }()

	e := newMetered(t)
	e.enroll("pod-1", "s1", e.token(t, "pod-1", "acct-A", time.Hour))
	conn, _, err := e.dial("pod-1", "s1", e.token(t, "pod-1", "acct-A", time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if f, _ := proto.ReadFrameWS(conn); f == nil || f.Kind != proto.KindCTL {
		t.Fatal("expected welcome")
	}
	e.advance(2 * time.Minute)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	sawNotice := false
	for {
		f, err := proto.ReadFrameWS(conn)
		if err != nil {
			break
		}
		if c, _ := proto.ParseControl(f); c != nil && c.Op == proto.OpError && strings.Contains(c.Message, "expired") {
			sawNotice = true
		}
	}
	if !sawNotice {
		t.Fatal("the Pod should be told why it was disconnected")
	}
	deadline := time.Now().Add(2 * time.Second)
	for e.srv.tunnels.GetTunnel("pod-1") != nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if e.srv.tunnels.GetTunnel("pod-1") != nil {
		t.Fatal("the tunnel should be gone")
	}
}

func TestEntitlementRenewalKeepsTunnelOpen(t *testing.T) {
	old := entitlementCheckEvery
	entitlementCheckEvery = 20 * time.Millisecond
	defer func() { entitlementCheckEvery = old }()

	e := newMetered(t)
	e.enroll("pod-1", "s1", e.token(t, "pod-1", "acct-A", time.Hour))
	conn, _, err := e.dial("pod-1", "s1", e.token(t, "pod-1", "acct-A", time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	proto.ReadFrameWS(conn) // welcome

	renewed := e.token(t, "pod-1", "acct-A", time.Hour)
	if err := proto.WriteCTLWS(conn, 0, &proto.Control{Op: proto.OpEntitlement, Entitlement: renewed}); err != nil {
		t.Fatal(err)
	}
	f, err := proto.ReadFrameWS(conn)
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := proto.ParseControl(f); c == nil || c.Op != proto.OpEntitlementOK || c.ExpiresAt == 0 {
		t.Fatalf("expected entitlement_ok, got %+v", c)
	}
	e.advance(10 * time.Minute) // past the first token, inside the renewed one
	time.Sleep(150 * time.Millisecond)
	if e.srv.tunnels.GetTunnel("pod-1") == nil {
		t.Fatal("a renewed tunnel should stay open")
	}
}

func TestRenewalFromAnotherAccountIsRefused(t *testing.T) {
	e := newMetered(t)
	e.enroll("pod-1", "s1", e.token(t, "pod-1", "acct-A", time.Hour))
	conn, _, err := e.dial("pod-1", "s1", e.token(t, "pod-1", "acct-A", time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	proto.ReadFrameWS(conn)
	proto.WriteCTLWS(conn, 0, &proto.Control{Op: proto.OpEntitlement, Entitlement: e.token(t, "pod-1", "acct-B", time.Hour)})
	f, _ := proto.ReadFrameWS(conn)
	if c, _ := proto.ParseControl(f); c == nil || c.Op != proto.OpError {
		t.Fatalf("a renewal from another account must be refused, got %+v", c)
	}
}

func TestEnrollIsRateLimited(t *testing.T) {
	srv, _ := NewServer(Config{RegistryPath: tempRegistry(t), AdminSecret: "admin", EnrollPerMinute: 3})
	ts := httptest.NewServer(http.HandlerFunc(srv.HandleHTTP))
	defer ts.Close()
	var last int
	for i := 0; i < 5; i++ {
		body := `{"device_id":"d","device_secret":"s","admin_token":"wrong"}`
		resp, _ := http.Post(ts.URL+"/v1/enroll", "application/json", strings.NewReader(body))
		last = resp.StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("guessing the admin token should be limited, got %d", last)
	}
}

// A sealed exchange: the relay must hand the Pod exactly the bytes the app
// sent, and hand the app exactly the bytes the Pod sent, with nothing readable.
func TestSealedExchangeIsOpaqueToTheRelay(t *testing.T) {
	srv, _ := NewServer(Config{RegistryPath: tempRegistry(t)})
	secret, _ := srv.registry.Add("pod-1", "x")
	ts := httptest.NewServer(http.HandlerFunc(srv.HandleHTTP))
	defer ts.Close()
	conn := wsDial(t, ts, "pod-1", secret)
	defer conn.Close()
	proto.ReadFrameWS(conn) // welcome
	srv.tunnels.SetClients("pod-1", []ClientBinding{{TokenHash: HashToken("tok")}})

	pod, _ := relaycrypto.NewIdentity()
	phone, env, err := relaycrypto.SealRequest(pod.Public(), []byte(`{"path":"/v1/secret-thing","body":"my diary"}`))
	if err != nil {
		t.Fatal(err)
	}

	// The Pod's side, speaking the tunnel protocol.
	podSaw := make(chan []byte, 1)
	go func() {
		var stream uint64
		var got []byte
		for {
			f, err := proto.ReadFrameWS(conn)
			if err != nil {
				return
			}
			switch f.Kind {
			case proto.KindOPEN:
				stream = f.StreamID
				meta, _ := proto.ParseHTTPMeta(f)
				if meta.Type != proto.StreamSealed || meta.Path != "" || len(meta.Headers) != 0 {
					t.Errorf("the relay must tell the Pod nothing about the request, got %+v", meta)
				}
			case proto.KindDATA:
				got = append(got, f.Payload...)
			case proto.KindEND:
				podSaw <- got
				sess, plain, _, err := pod.OpenRequest(got)
				if err != nil || !strings.Contains(string(plain), "my diary") {
					t.Errorf("the Pod should open what the phone sealed: %v", err)
				}
				proto.WriteOPENWS(conn, stream, &proto.HTTPResponseMeta{Type: proto.StreamSealed, Status: 200})
				for _, m := range []struct {
					k byte
					b string
				}{{relaycrypto.MsgHead, `{"status":200}`}, {relaycrypto.MsgData, "private answer"}, {relaycrypto.MsgEnd, ""}} {
					s, _ := sess.SealMessage(m.k, []byte(m.b))
					proto.WriteDATAWS(conn, stream, relaycrypto.Frame(s))
				}
				proto.WriteENDWS(conn, stream)
				return
			}
		}
	}()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/sealed", bytes.NewReader(env))
	req.Header.Set("X-Ghost-Client-Id", "pod-1")
	req.Header.Set("X-Ghost-Client-Token", "tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	wire, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 || resp.Header.Get("X-Ghost-Relay") != "sealed" {
		t.Fatalf("unexpected response %d %v", resp.StatusCode, resp.Header)
	}
	if got := <-podSaw; !bytes.Equal(got, env) {
		t.Fatal("the relay changed the request on the way to the Pod")
	}
	if bytes.Contains(wire, []byte("private answer")) {
		t.Fatal("the response crossed the relay readable")
	}
	frames, rest := relaycrypto.SplitFrames(wire)
	if len(rest) != 0 || len(frames) != 3 {
		t.Fatalf("expected 3 whole frames, got %d with %d left over", len(frames), len(rest))
	}
	var body []byte
	for i, f := range frames {
		kind, content, err := phone.OpenMessage(f)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if kind == relaycrypto.MsgData {
			body = append(body, content...)
		}
	}
	if string(body) != "private answer" {
		t.Fatalf("got %q", body)
	}
}

func TestSealedNeedsAPairedClient(t *testing.T) {
	srv, _ := NewServer(Config{RegistryPath: tempRegistry(t)})
	srv.tunnels.SetClients("pod-1", []ClientBinding{{TokenHash: HashToken("tok")}})
	for name, hdr := range map[string]http.Header{
		"no credentials": {},
		"wrong token":    {"X-Ghost-Client-Id": {"pod-1"}, "X-Ghost-Client-Token": {"nope"}},
	} {
		req := httptest.NewRequest("POST", "/v1/sealed", strings.NewReader("x"))
		req.Header = hdr
		w := httptest.NewRecorder()
		srv.HandleHTTP(w, req)
		if w.Code != 401 {
			t.Errorf("%s: got %d, want 401", name, w.Code)
		}
	}
	req := httptest.NewRequest("POST", "/v1/sealed", strings.NewReader("x"))
	req.Header.Set("X-Ghost-Client-Id", "pod-1")
	req.Header.Set("X-Ghost-Client-Token", "tok")
	w := httptest.NewRecorder()
	srv.HandleHTTP(w, req)
	if w.Code != 503 {
		t.Errorf("a paired client with the Pod offline should get 503, got %d", w.Code)
	}
}

func TestHealthSaysWhetherMetered(t *testing.T) {
	e := newMetered(t)
	resp, _ := http.Get(e.ts.URL + "/v1/health")
	var h map[string]any
	json.NewDecoder(resp.Body).Decode(&h)
	if h["metered"] != true {
		t.Fatalf("health should say the relay is metered: %v", h)
	}
}
