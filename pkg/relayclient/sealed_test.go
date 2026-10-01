package relayclient

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/entitlement"
	"github.com/ianclemence/ghost/pkg/relay/server"
	"github.com/ianclemence/ghost/pkg/relaycrypto"
)

// tap sits in front of the relay and keeps everything that crosses it, which
// is exactly what a hostile relay operator would see.
type tap struct {
	mu       sync.Mutex
	requests [][]byte
	replies  [][]byte
	next     http.Handler
	cutTail  int // bytes to drop from the end of a sealed reply
}

func (p *tap) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/sealed" && !strings.HasPrefix(r.URL.Path, "/v1/") || r.Header.Get("Upgrade") != "" {
		p.next.ServeHTTP(w, r)
		return
	}
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	rec := httptest.NewRecorder()
	p.next.ServeHTTP(rec, r)
	out := rec.Body.Bytes()
	p.mu.Lock()
	p.requests = append(p.requests, body)
	p.replies = append(p.replies, append([]byte(nil), out...))
	p.mu.Unlock()
	if p.cutTail > 0 && len(out) > p.cutTail {
		out = out[:len(out)-p.cutTail]
	}
	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	w.WriteHeader(rec.Code)
	w.Write(out)
}

func (p *tap) seen() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	var all []byte
	for _, b := range p.requests {
		all = append(all, b...)
	}
	for _, b := range p.replies {
		all = append(all, b...)
	}
	return all
}

type podEnv struct {
	relayURL string
	tap      *tap
	client   *Client
	podPub   []byte
	ghostID  string
	tokens   map[string]string
	gotVia   chan string
	cancel   context.CancelFunc
}

func gateway(t *testing.T, via chan string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		select {
		case via <- r.Header.Get("X-Ghost-Via"):
		default:
		}
		w.Header().Set("X-Seen-Auth", r.Header.Get("Authorization"))
		w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/v1/chat", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Write(append([]byte("echo:"), b...))
	})
	mux.HandleFunc("/v1/permissions/grant", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("granted")) })
	mux.HandleFunc("/v1/stream", func(w http.ResponseWriter, r *http.Request) {
		f := w.(http.Flusher)
		for i := 0; i < 4; i++ {
			w.Write([]byte(strings.Repeat("x", 40000)))
			f.Flush()
		}
	})
	gw := httptest.NewServer(mux)
	t.Cleanup(gw.Close)
	return gw
}

func startPod(t *testing.T, cfg server.Config, clientCfg func(*ClientConfig)) *podEnv {
	t.Helper()
	t.Setenv("GHOST_DIR", t.TempDir())
	cfg.RegistryPath = t.TempDir() + "/registry.json"
	srv, err := server.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := server.NewRegistry(cfg.RegistryPath)
	secret, _ := reg.Add("ghost-1", "pod")
	srv, _ = server.NewServer(cfg) // reload so the relay sees the registered device

	p := &tap{next: http.HandlerFunc(srv.HandleHTTP)}
	ts := httptest.NewServer(p)
	t.Cleanup(ts.Close)

	id, _ := relaycrypto.NewIdentity()
	env := &podEnv{relayURL: ts.URL, tap: p, podPub: id.Public(), ghostID: "ghost-1",
		tokens: map[string]string{}, gotVia: make(chan string, 4)}
	env.tokens["full"], _ = AddClientScoped("ghost-1", "phone", "full")
	env.tokens["chat"], _ = AddClientScoped("ghost-1", "watch", "chat")

	gw := gateway(t, env.gotVia)
	cc := ClientConfig{DeviceID: "ghost-1", DeviceSecret: secret, RelayServer: ts.URL, GatewayURL: gw.URL,
		ReconnectMin: 1, ReconnectMax: 1, Identity: id, RequireSealed: true}
	if clientCfg != nil {
		clientCfg(&cc)
	}
	env.client = NewClient(cc)
	ctx, cancel := context.WithCancel(context.Background())
	env.cancel = cancel
	t.Cleanup(func() { cancel(); env.client.Stop() })
	go env.client.Run(ctx)
	return env
}

func (e *podEnv) waitConnected(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if e.client.Status().State == StateConnected {
			// Let the relay take the client list the Pod sends on connect.
			time.Sleep(150 * time.Millisecond)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("never connected: %+v", e.client.Status())
}

func (e *podEnv) do(t *testing.T, which string, req SealedRequest) (*SealedResponse, error) {
	t.Helper()
	return DoSealed(e.relayURL, e.ghostID, e.tokens[which], e.podPub, req)
}

func TestSealedRoundTripThroughRelay(t *testing.T) {
	e := startPod(t, server.Config{}, nil)
	e.waitConnected(t)

	resp, err := e.do(t, "full", SealedRequest{Method: "POST", Path: "/v1/chat", Body: EncodeBody([]byte("my deepest secret"))})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 || string(resp.Body) != "echo:my deepest secret" {
		t.Fatalf("unexpected response %d %q", resp.Status, resp.Body)
	}

	resp, err = e.do(t, "full", SealedRequest{Path: "/v1/health", Headers: map[string][]string{
		"Authorization": {"Bearer device-credential"}, "X-Ghost-Via": {"loopback"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Headers["X-Seen-Auth"]; len(got) != 1 || got[0] != "Bearer device-credential" {
		t.Fatalf("the device credential should reach the gateway, got %v", got)
	}
	if via := <-e.gotVia; via != "relay" {
		t.Fatalf("a relayed request must be marked as relayed even if it claims otherwise, got %q", via)
	}

	// What the relay operator saw: bytes, and nothing readable in them.
	seen := e.tap.seen()
	for _, secret := range []string{"my deepest secret", "echo:", "/v1/chat", "/v1/health", "device-credential", e.tokens["full"]} {
		if bytes.Contains(seen, []byte(secret)) {
			t.Errorf("the relay could read %q", secret)
		}
	}
}

func TestPlainTrafficRefusedWhenSealingIsRequired(t *testing.T) {
	e := startPod(t, server.Config{}, nil)
	e.waitConnected(t)
	req, _ := http.NewRequest("POST", e.relayURL+"/v1/chat", strings.NewReader("hi"))
	req.Header.Set("X-Ghost-Client-Id", e.ghostID)
	req.Header.Set("X-Ghost-Client-Token", e.tokens["full"])
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == 200 {
		t.Fatal("a Pod that requires sealing must not answer plain requests")
	}
}

func TestPlainTrafficStillWorksOnYourOwnRelay(t *testing.T) {
	e := startPod(t, server.Config{}, func(c *ClientConfig) { c.RequireSealed = false })
	e.waitConnected(t)
	req, _ := http.NewRequest("POST", e.relayURL+"/v1/chat", strings.NewReader("hi"))
	req.Header.Set("X-Ghost-Client-Id", e.ghostID)
	req.Header.Set("X-Ghost-Client-Token", e.tokens["full"])
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("self-hosted plain traffic should keep working: %v %v", resp, err)
	}
}

func TestPodEnforcesScopeInsideTheEnvelope(t *testing.T) {
	e := startPod(t, server.Config{}, nil)
	e.waitConnected(t)
	// The relay cannot see the path, so it cannot enforce scope. The Pod does.
	if _, err := e.do(t, "chat", SealedRequest{Method: "POST", Path: "/v1/permissions/grant"}); err == nil {
		t.Fatal("a chat-scoped client must not reach permissions")
	}
	if _, err := e.do(t, "chat", SealedRequest{Method: "POST", Path: "/v1/chat", Body: EncodeBody([]byte("hi"))}); err != nil {
		t.Fatalf("a chat-scoped client may chat: %v", err)
	}
	if _, err := e.do(t, "full", SealedRequest{Method: "POST", Path: "/v1/permissions/grant"}); err != nil {
		t.Fatalf("a full-scope client may: %v", err)
	}
}

func TestPodChecksTheTokenInsideTheEnvelope(t *testing.T) {
	e := startPod(t, server.Config{}, nil)
	e.waitConnected(t)
	// A valid header token (so the relay lets it through) but a different token
	// sealed inside: the Pod must go by the sealed one.
	_, err := doSealedRaw(e.relayURL, e.ghostID, e.tokens["full"], e.podPub, SealedRequest{
		Timestamp: time.Now().UnixMilli(), Method: "GET", Path: "/v1/health", Token: "not-a-paired-token"})
	if err == nil {
		t.Fatal("an unknown sealed token must be refused")
	}
}

func TestPodRefusesTraversalAndNonAPIPaths(t *testing.T) {
	e := startPod(t, server.Config{}, nil)
	e.waitConnected(t)
	for _, p := range []string{"/v1/../admin", "/admin", "/oauth/x", "v1/health", ""} {
		if _, err := e.do(t, "full", SealedRequest{Path: p}); err == nil {
			t.Errorf("path %q should be refused", p)
		}
	}
}

func TestReplayedRequestIsRefused(t *testing.T) {
	e := startPod(t, server.Config{}, nil)
	e.waitConnected(t)
	if _, err := e.do(t, "full", SealedRequest{Method: "POST", Path: "/v1/chat", Body: EncodeBody([]byte("pay"))}); err != nil {
		t.Fatal(err)
	}
	// A hostile relay re-sends the exact bytes it captured, in a fresh exchange.
	e.tap.mu.Lock()
	captured := e.tap.requests[len(e.tap.requests)-1]
	e.tap.mu.Unlock()
	req, _ := http.NewRequest("POST", e.relayURL+"/v1/sealed", bytes.NewReader(captured))
	req.Header.Set("X-Ghost-Client-Id", e.ghostID)
	req.Header.Set("X-Ghost-Client-Token", e.tokens["full"])
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == 200 {
		t.Fatal("a replayed sealed request must not be answered")
	}
}

func TestStaleRequestIsRefused(t *testing.T) {
	e := startPod(t, server.Config{}, nil)
	e.waitConnected(t)
	_, err := e.do(t, "full", SealedRequest{Path: "/v1/health", Timestamp: time.Now().Add(-time.Hour).UnixMilli()})
	if err == nil {
		t.Fatal("a request stamped an hour ago must be refused")
	}
}

func TestLargeStreamedResponseArrivesWhole(t *testing.T) {
	e := startPod(t, server.Config{}, nil)
	e.waitConnected(t)
	resp, err := e.do(t, "full", SealedRequest{Path: "/v1/stream"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Body) != 160000 {
		t.Fatalf("got %d bytes, want 160000", len(resp.Body))
	}
}

func TestACutResponseIsDetected(t *testing.T) {
	e := startPod(t, server.Config{}, nil)
	e.waitConnected(t)
	e.tap.cutTail = 25 // the relay drops the closing message
	_, err := e.do(t, "full", SealedRequest{Path: "/v1/health"})
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("a response without its end must be reported as cut short, got %v", err)
	}
}

func TestHostedRelayWantsAnEntitlement(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	cfg := server.Config{EntitlementKeys: []ed25519.PublicKey{pub}}

	// Without a pass the Pod is told to pay, and says so.
	e := startPod(t, cfg, nil)
	deadline := time.Now().Add(5 * time.Second)
	for e.client.Status().State != StateNeedsPayment && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if st := e.client.Status(); st.State != StateNeedsPayment {
		t.Fatalf("expected needs-payment, got %+v", st)
	}

	// With one it connects, and a renewal sent while connected is accepted.
	now := time.Now()
	mint := func(ttl time.Duration) string {
		tok, _ := entitlement.Sign(priv, entitlement.Claims{Pod: "ghost-1", Account: "acct", IssuedAt: now.Unix(), ExpiresAt: now.Add(ttl).Unix()})
		return tok
	}
	e2 := startPod(t, cfg, func(c *ClientConfig) { c.Entitlement = mint(time.Hour) })
	// The device must be enrolled with the relay's registry; startPod did that
	// through the operator path, which a metered relay still honours for known
	// devices.
	e2.waitConnected(t)
	e2.client.UpdateEntitlement(mint(2 * time.Hour))
	time.Sleep(200 * time.Millisecond)
	if st := e2.client.Status(); st.State != StateConnected {
		t.Fatalf("renewal must not drop the tunnel: %+v", st)
	}
}

func TestSealedBodyJSONShapeIsStable(t *testing.T) {
	// The app builds this JSON by hand in TypeScript; if a field is renamed here
	// the phone silently stops working, so the names are pinned.
	b, _ := json.Marshal(SealedRequest{Timestamp: 1, Method: "GET", Path: "/v1/x", Query: "a=b",
		Headers: map[string][]string{"A": {"b"}}, Body: "Yg==", Token: "t"})
	want := `{"ts":1,"method":"GET","path":"/v1/x","query":"a=b","headers":{"A":["b"]},"body":"Yg==","token":"t"}`
	if string(b) != want {
		t.Fatalf("\n got %s\nwant %s", b, want)
	}
	h, _ := json.Marshal(SealedHead{Status: 200, Headers: map[string][]string{"A": {"b"}}})
	if string(h) != `{"status":200,"headers":{"A":["b"]}}` {
		t.Fatalf("head shape changed: %s", h)
	}
}
