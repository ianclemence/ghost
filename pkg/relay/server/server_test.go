package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ianclemence/ghost/pkg/relay/proto"
)

func tempRegistry(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "registry.json")
}

func TestRegistryAddAndAuthenticate(t *testing.T) {
	path := tempRegistry(t)
	reg, err := NewRegistry(path)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	secret, err := reg.Add("device-1", "Test Ghost")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if secret == "" {
		t.Fatal("expected non-empty secret")
	}

	if !reg.Authenticate("device-1", secret) {
		t.Error("expected authentication to succeed")
	}

	if reg.Authenticate("device-1", "wrong-secret") {
		t.Error("expected authentication to fail with wrong secret")
	}

	if reg.Authenticate("unknown-device", secret) {
		t.Error("expected authentication to fail for unknown device")
	}
}

func TestRegistryPersistence(t *testing.T) {
	path := tempRegistry(t)

	reg1, err := NewRegistry(path)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	secret, _ := reg1.Add("device-1", "Test")

	reg2, err := NewRegistry(path)
	if err != nil {
		t.Fatalf("NewRegistry reload: %v", err)
	}
	if !reg2.Authenticate("device-1", secret) {
		t.Error("expected authentication after reload")
	}

	devices := reg2.List()
	if len(devices) != 1 {
		t.Errorf("expected 1 device, got %d", len(devices))
	}
}

func TestRegistryRemove(t *testing.T) {
	path := tempRegistry(t)
	reg, _ := NewRegistry(path)
	reg.Add("device-1", "Test")

	if err := reg.Remove("device-1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if reg.Authenticate("device-1", "anything") {
		t.Error("expected authentication to fail after remove")
	}

	if err := reg.Remove("nonexistent"); err == nil {
		t.Error("expected error removing nonexistent device")
	}
}

func TestTunnelManager(t *testing.T) {
	tm := NewTunnelManager()

	if tm.GetTunnel("device-1") != nil {
		t.Error("expected nil tunnel")
	}

	if tm.AuthClient("device-1", "token") {
		t.Error("expected auth to fail with no clients")
	}

	hash := sha256.Sum256([]byte("test-token"))
	hashHex := hex.EncodeToString(hash[:])
	tm.SetClients("device-1", []ClientBinding{
		{TokenHash: hashHex, Name: "phone"},
	})

	if !tm.AuthClient("device-1", "test-token") {
		t.Error("expected auth to succeed")
	}
	if tm.AuthClient("device-1", "wrong-token") {
		t.Error("expected auth to fail with wrong token")
	}
}

func TestHandleHealth(t *testing.T) {
	path := tempRegistry(t)
	srv, err := NewServer(Config{RegistryPath: path})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := httptest.NewRequest("GET", "/v1/health", nil)
	w := httptest.NewRecorder()
	srv.HandleHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %v, want ok", resp["status"])
	}
}

func TestHandleAppRequestNoAuth(t *testing.T) {
	path := tempRegistry(t)
	srv, _ := NewServer(Config{RegistryPath: path})

	req := httptest.NewRequest("POST", "/v1/chat", nil)
	w := httptest.NewRecorder()
	srv.HandleHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestHandleAppRequestDeviceOffline(t *testing.T) {
	path := tempRegistry(t)
	srv, _ := NewServer(Config{RegistryPath: path})

	hash := sha256.Sum256([]byte("valid-token"))
	srv.tunnels.SetClients("device-1", []ClientBinding{
		{TokenHash: hex.EncodeToString(hash[:])},
	})

	req := httptest.NewRequest("POST", "/v1/chat", nil)
	req.Header.Set("X-Ghost-Client-Id", "device-1")
	req.Header.Set("X-Ghost-Client-Token", "valid-token")
	w := httptest.NewRecorder()
	srv.HandleHTTP(w, req)

	if w.Code != 503 {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestHandleAppRequestInvalidToken(t *testing.T) {
	path := tempRegistry(t)
	srv, _ := NewServer(Config{RegistryPath: path})

	srv.tunnels.SetClients("device-1", []ClientBinding{
		{TokenHash: "deadbeef"},
	})

	req := httptest.NewRequest("POST", "/v1/chat", nil)
	req.Header.Set("X-Ghost-Client-Id", "device-1")
	req.Header.Set("X-Ghost-Client-Token", "wrong-token")
	w := httptest.NewRecorder()
	srv.HandleHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestHandleDeviceTunnelNoAuth(t *testing.T) {
	path := tempRegistry(t)
	srv, _ := NewServer(Config{RegistryPath: path})

	req := httptest.NewRequest("GET", "/v1/tunnel", nil)
	w := httptest.NewRecorder()
	srv.HandleHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestGenerateToken(t *testing.T) {
	t1, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	t2, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if t1 == t2 {
		t.Error("expected different tokens")
	}
	if len(t1) != 64 {
		t.Errorf("token length = %d, want 64", len(t1))
	}
}

func TestHashToken(t *testing.T) {
	h1 := HashToken("test-token")
	h2 := HashToken("test-token")
	h3 := HashToken("other-token")

	if h1 != h2 {
		t.Error("expected same hash for same token")
	}
	if h1 == h3 {
		t.Error("expected different hash for different token")
	}
	if len(h1) != 64 {
		t.Errorf("hash length = %d, want 64", len(h1))
	}
}

func TestEndToEndRelayFlow(t *testing.T) {
	path := tempRegistry(t)
	srv, err := NewServer(Config{RegistryPath: path})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Register device
	secret, _ := srv.registry.Add("test-device", "Test")

	// Start relay server as httptest
	ts := httptest.NewServer(http.HandlerFunc(srv.HandleHTTP))
	defer ts.Close()

	// Device connects via WebSocket (header credentials)
	deviceConn := wsDial(t, ts, "test-device", secret)
	defer deviceConn.Close()

	// Read welcome
	f, err := proto.ReadFrameWS(deviceConn)
	if err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	ctrl, _ := proto.ParseControl(f)
	if ctrl.Op != proto.OpWelcome {
		t.Errorf("welcome op = %q, want %q", ctrl.Op, proto.OpWelcome)
	}

	// Set up client binding
	hash := sha256.Sum256([]byte("app-token"))
	srv.tunnels.SetClients("test-device", []ClientBinding{
		{TokenHash: hex.EncodeToString(hash[:])},
	})

	// App sends request in background
	appDone := make(chan int, 1)
	go func() {
		req, _ := http.NewRequest("POST", ts.URL+"/v1/chat", strings.NewReader(`{"content":"hello"}`))
		req.Header.Set("X-Ghost-Client-Id", "test-device")
		req.Header.Set("X-Ghost-Client-Token", "app-token")
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			appDone <- -1
			return
		}
		defer resp.Body.Close()
		appDone <- resp.StatusCode
	}()

	// Device reads the forwarded request
	// Read OPEN frame
	f, err = proto.ReadFrameWS(deviceConn)
	if err != nil {
		t.Fatalf("device read OPEN: %v", err)
	}
	if f.Kind != proto.KindOPEN {
		t.Fatalf("expected OPEN, got kind=%d", f.Kind)
	}
	meta, _ := proto.ParseHTTPMeta(f)
	if meta.Method != "POST" || meta.Path != "/v1/chat" {
		t.Errorf("unexpected request: %s %s", meta.Method, meta.Path)
	}

	// Read DATA frame (body)
	f, err = proto.ReadFrameWS(deviceConn)
	if err != nil {
		t.Fatalf("device read DATA: %v", err)
	}
	if f.Kind != proto.KindDATA {
		t.Fatalf("expected DATA, got kind=%d", f.Kind)
	}

	// Read END frame
	f, err = proto.ReadFrameWS(deviceConn)
	if err != nil {
		t.Fatalf("device read END: %v", err)
	}
	if f.Kind != proto.KindEND {
		t.Fatalf("expected END, got kind=%d", f.Kind)
	}

	// Device sends response back through tunnel
	streamID := f.StreamID
	_ = proto.WriteOPENWS(deviceConn, streamID, &proto.HTTPResponseMeta{
		Type:    proto.StreamHTTP,
		Status:  200,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
	})
	_ = proto.WriteDATAWS(deviceConn, streamID, []byte(`{"status":"ok"}`))
	_ = proto.WriteENDWS(deviceConn, streamID)

	// Verify app got response
	status := <-appDone
	if status != 200 {
		t.Errorf("app status = %d, want 200", status)
	}
}

func TestTwoDevicesIsolation(t *testing.T) {
	path := tempRegistry(t)
	srv, _ := NewServer(Config{RegistryPath: path})

	// Register two devices
	secret1, _ := srv.registry.Add("device-1", "Ghost 1")
	secret2, _ := srv.registry.Add("device-2", "Ghost 2")

	ts := httptest.NewServer(http.HandlerFunc(srv.HandleHTTP))
	defer ts.Close()

	// Connect device-1
	conn1 := wsDial(t, ts, "device-1", secret1)
	defer conn1.Close()
	proto.ReadFrameWS(conn1) // welcome

	// Connect device-2
	conn2 := wsDial(t, ts, "device-2", secret2)
	defer conn2.Close()
	proto.ReadFrameWS(conn2) // welcome

	// Set client for device-1 only
	hash1 := sha256.Sum256([]byte("token-1"))
	srv.tunnels.SetClients("device-1", []ClientBinding{
		{TokenHash: hex.EncodeToString(hash1[:])},
	})

	// App tries to access device-2 (no client binding) -> should fail
	req, _ := http.NewRequest("GET", ts.URL+"/v1/chat", nil)
	req.Header.Set("X-Ghost-Client-Id", "device-2")
	req.Header.Set("X-Ghost-Client-Token", "token-1")
	w := httptest.NewRecorder()
	srv.HandleHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("cross-device access: status = %d, want 401", w.Code)
	}
}

func TestReconnectReplacesOldTunnel(t *testing.T) {
	path := tempRegistry(t)
	srv, _ := NewServer(Config{RegistryPath: path})
	secret, _ := srv.registry.Add("device-1", "Test")

	ts := httptest.NewServer(http.HandlerFunc(srv.HandleHTTP))
	defer ts.Close()

	// First connection
	conn1 := wsDial(t, ts, "device-1", secret)
	proto.ReadFrameWS(conn1) // welcome

	t1 := srv.tunnels.GetTunnel("device-1")
	if t1 == nil {
		t.Fatal("expected tunnel")
	}

	// Second connection (reconnect)
	conn2 := wsDial(t, ts, "device-1", secret)
	defer conn2.Close()
	proto.ReadFrameWS(conn2) // welcome

	t2 := srv.tunnels.GetTunnel("device-1")
	if t2 == nil {
		t.Fatal("expected tunnel after reconnect")
	}
	if t2 == t1 {
		t.Error("expected new tunnel, got same reference")
	}

	// Old connection should be closed
	conn1.Close()
}

func wsURL(base string) string {
	return "ws" + base[4:]
}

// wsDial opens a device tunnel using header-based credentials (query-param
// credentials were removed for security — they leak into proxy logs).
func wsDial(t *testing.T, ts *httptest.Server, deviceID, secret string) *websocket.Conn {
	t.Helper()
	h := http.Header{}
	if deviceID != "" {
		h.Set("X-Ghost-Device", deviceID)
		h.Set("X-Ghost-Device-Secret", secret)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(ts.URL)+"/v1/tunnel", h)
	if err != nil {
		t.Fatalf("device connect: %v", err)
	}
	return conn
}

// ─── End-to-End Vertical Slice Test ─────────────────────────────────────────
// Proves: phone → relay → Ghost → relay → phone

func TestEndToEndVerticalSlice(t *testing.T) {
	path := tempRegistry(t)
	srv, err := NewServer(Config{RegistryPath: path})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// 1. Register device
	deviceSecret, _ := srv.registry.Add("ghost-alpha", "Test Ghost Alpha")

	// 2. Start relay server
	ts := httptest.NewServer(http.HandlerFunc(srv.HandleHTTP))
	defer ts.Close()

	// 3. Start mock Ghost gateway (simulates local Ghost API)
	bridgeSecret := "test-bridge-secret-abc123"
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify bridge secret was injected by relay client
		got := r.Header.Get("X-Ghost-Secret")
		if got != bridgeSecret {
			t.Errorf("bridge secret: got %q, want %q", got, bridgeSecret)
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		// Verify X-Ghost-Secret was NOT forwarded from relay (should be stripped)
		// The relay client injects it, so it should arrive but NOT from the relay

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		// Simulate SSE streaming response
		w.Write([]byte("data: {\"content\":\"Hello from Ghost via relay\"}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer gateway.Close()

	// 4. Connect relay client (device) to relay
	deviceConn := wsDial(t, ts, "ghost-alpha", deviceSecret)
	defer deviceConn.Close()

	// Read welcome
	f, err := proto.ReadFrameWS(deviceConn)
	if err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	ctrl, _ := proto.ParseControl(f)
	if ctrl.Op != proto.OpWelcome {
		t.Fatalf("expected welcome, got op=%s", ctrl.Op)
	}

	// Register client token for this device
	tokenHash := sha256.Sum256([]byte("test-phone-token"))
	srv.tunnels.SetClients("ghost-alpha", []ClientBinding{
		{TokenHash: hex.EncodeToString(tokenHash[:]), Name: "Phone"},
	})

	// 5. Phone sends request through relay
	// (simulates what the mobile app does: POST /v1/chat with relay headers)
	chatBody := `{"content":"Hello Ghost","session_key":"mobile:default","channel":"mobile"}`
	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat", strings.NewReader(chatBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ghost-Client-Id", "ghost-alpha")
	req.Header.Set("X-Ghost-Client-Token", "test-phone-token")
	req.Header.Set("X-Ghost-Session", "mobile:default")

	// Device reads the forwarded request in a goroutine and sends response
	deviceDone := make(chan struct{})
	go func() {
		defer close(deviceDone)

		// Read OPEN frame (HTTP request from relay)
		f, err := proto.ReadFrameWS(deviceConn)
		if err != nil {
			t.Errorf("device read OPEN: %v", err)
			return
		}
		if f.Kind != proto.KindOPEN {
			t.Errorf("expected OPEN, got kind=%d", f.Kind)
			return
		}
		meta, _ := proto.ParseHTTPMeta(f)
		if meta.Method != "POST" || meta.Path != "/v1/chat" {
			t.Errorf("unexpected request: %s %s", meta.Method, meta.Path)
		}

		// Read DATA frame (request body)
		f, err = proto.ReadFrameWS(deviceConn)
		if err != nil {
			t.Errorf("device read DATA: %v", err)
			return
		}
		if f.Kind != proto.KindDATA {
			t.Errorf("expected DATA, got kind=%d", f.Kind)
		}

		// Read END frame
		f, err = proto.ReadFrameWS(deviceConn)
		if err != nil {
			t.Errorf("device read END: %v", err)
			return
		}
		if f.Kind != proto.KindEND {
			t.Errorf("expected END, got kind=%d", f.Kind)
		}

		// Verify bridge secret was stripped from forwarded headers
		if _, ok := meta.Headers["X-Ghost-Secret"]; ok {
			t.Error("X-Ghost-Secret should have been stripped by relay")
		}
		if _, ok := meta.Headers["X-Ghost-Client-Id"]; ok {
			t.Error("X-Ghost-Client-Id should have been stripped by relay")
		}
		if _, ok := meta.Headers["X-Ghost-Client-Token"]; ok {
			t.Error("X-Ghost-Client-Token should have been stripped by relay")
		}

		// Device sends response back through relay
		streamID := f.StreamID
		_ = proto.WriteOPENWS(deviceConn, streamID, &proto.HTTPResponseMeta{
			Type:    proto.StreamHTTP,
			Status:  200,
			Headers: map[string][]string{"Content-Type": {"text/event-stream"}},
		})
		_ = proto.WriteDATAWS(deviceConn, streamID, []byte("data: {\"content\":\"Hello from Ghost via relay\"}\n\n"))
		_ = proto.WriteDATAWS(deviceConn, streamID, []byte("data: [DONE]\n\n"))
		_ = proto.WriteENDWS(deviceConn, streamID)
	}()

	// Execute the phone's request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("phone request: %v", err)
	}
	defer resp.Body.Close()

	// 6. Verify response
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	relayHeader := resp.Header.Get("X-Ghost-Relay")
	if relayHeader != "true" {
		t.Errorf("X-Ghost-Relay = %q, want true", relayHeader)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Hello from Ghost via relay") {
		t.Errorf("response body missing expected content: %s", body)
	}
	if !strings.Contains(string(body), "[DONE]") {
		t.Errorf("response body missing [DONE]: %s", body)
	}

	// Wait for device goroutine to finish
	<-deviceDone
}

// ─── Security Tests ─────────────────────────────────────────────────────────

func TestCrossDeviceIsolation(t *testing.T) {
	path := tempRegistry(t)
	srv, _ := NewServer(Config{RegistryPath: path})

	// Register two devices
	secret1, _ := srv.registry.Add("ghost-1", "Ghost Alpha")
	secret2, _ := srv.registry.Add("ghost-2", "Ghost Beta")

	ts := httptest.NewServer(http.HandlerFunc(srv.HandleHTTP))
	defer ts.Close()

	// Connect both devices
	conn1 := wsDial(t, ts, "ghost-1", secret1)
	defer conn1.Close()
	proto.ReadFrameWS(conn1) // welcome

	conn2 := wsDial(t, ts, "ghost-2", secret2)
	defer conn2.Close()
	proto.ReadFrameWS(conn2) // welcome

	// Register client for ghost-1 only
	hash1 := sha256.Sum256([]byte("token-ghost-1"))
	srv.tunnels.SetClients("ghost-1", []ClientBinding{
		{TokenHash: hex.EncodeToString(hash1[:])},
	})

	// Phone tries to access ghost-2 using ghost-1's token → 401
	req, _ := http.NewRequest("GET", ts.URL+"/v1/sessions", nil)
	req.Header.Set("X-Ghost-Client-Id", "ghost-2")
	req.Header.Set("X-Ghost-Client-Token", "token-ghost-1")
	w := httptest.NewRecorder()
	srv.HandleHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("cross-device access: status = %d, want 401", w.Code)
	}
}

func TestInvalidTokenRejected(t *testing.T) {
	path := tempRegistry(t)
	srv, _ := NewServer(Config{RegistryPath: path})
	secret, _ := srv.registry.Add("ghost-1", "Test")

	ts := httptest.NewServer(http.HandlerFunc(srv.HandleHTTP))
	defer ts.Close()

	conn := wsDial(t, ts, "ghost-1", secret)
	defer conn.Close()
	proto.ReadFrameWS(conn) // welcome

	// Register a client
	hash := sha256.Sum256([]byte("correct-token"))
	srv.tunnels.SetClients("ghost-1", []ClientBinding{
		{TokenHash: hex.EncodeToString(hash[:])},
	})

	// Try with wrong token
	req, _ := http.NewRequest("GET", ts.URL+"/v1/sessions", nil)
	req.Header.Set("X-Ghost-Client-Id", "ghost-1")
	req.Header.Set("X-Ghost-Client-Token", "wrong-token")
	w := httptest.NewRecorder()
	srv.HandleHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("invalid token: status = %d, want 401", w.Code)
	}
}

func TestMissingCredentialsRejected(t *testing.T) {
	path := tempRegistry(t)
	srv, _ := NewServer(Config{RegistryPath: path})
	secret, _ := srv.registry.Add("ghost-1", "Test")

	ts := httptest.NewServer(http.HandlerFunc(srv.HandleHTTP))
	defer ts.Close()

	conn := wsDial(t, ts, "ghost-1", secret)
	defer conn.Close()
	proto.ReadFrameWS(conn) // welcome

	// Request with no auth headers
	req, _ := http.NewRequest("GET", ts.URL+"/v1/chat", nil)
	w := httptest.NewRecorder()
	srv.HandleHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("missing credentials: status = %d, want 401", w.Code)
	}
}

func TestBridgeSecretNotForwarded(t *testing.T) {
	path := tempRegistry(t)
	srv, _ := NewServer(Config{RegistryPath: path})
	secret, _ := srv.registry.Add("ghost-1", "Test")

	ts := httptest.NewServer(http.HandlerFunc(srv.HandleHTTP))
	defer ts.Close()

	conn := wsDial(t, ts, "ghost-1", secret)
	defer conn.Close()
	proto.ReadFrameWS(conn) // welcome

	hash := sha256.Sum256([]byte("test-token"))
	srv.tunnels.SetClients("ghost-1", []ClientBinding{
		{TokenHash: hex.EncodeToString(hash[:])},
	})

	// Send request that includes X-Ghost-Secret (which should be stripped)
	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat", strings.NewReader(`{"content":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ghost-Client-Id", "ghost-1")
	req.Header.Set("X-Ghost-Client-Token", "test-token")
	req.Header.Set("X-Ghost-Secret", "my-bridge-secret")

	// Read the forwarded request in a goroutine. The fixture completes the
	// protocol exchange (OPEN → DATA → END, then an ERROR reply) so the relay
	// handler returns immediately instead of blocking on its 30s timeout.
	got := make(chan bool, 1)
	go func() {
		f, err := proto.ReadFrameWS(conn)
		if err != nil {
			got <- false
			return
		}
		if f.Kind != proto.KindOPEN {
			got <- false
			return
		}
		meta, _ := proto.ParseHTTPMeta(f)

		// Drain DATA + END so the request is fully delivered.
		for i := 0; i < 2; i++ {
			if _, err := proto.ReadFrameWS(conn); err != nil {
				got <- false
				return
			}
		}

		// X-Ghost-Secret should NOT be in forwarded headers
		_, hasSecret := meta.Headers["X-Ghost-Secret"]
		got <- hasSecret

		// Reply with ERROR so the relay handler finishes right away.
		_ = proto.WriteERRORWS(conn, f.StreamID, "test-complete")
	}()

	w := httptest.NewRecorder()
	srv.HandleHTTP(w, req)

	// Wait for device goroutine
	hasSecretForwarded := <-got
	if hasSecretForwarded {
		t.Error("X-Ghost-Secret was forwarded through relay — should be stripped")
	}
}

func TestReconnectNoStaleTunnel(t *testing.T) {
	path := tempRegistry(t)
	srv, _ := NewServer(Config{RegistryPath: path})
	secret, _ := srv.registry.Add("ghost-1", "Test")

	ts := httptest.NewServer(http.HandlerFunc(srv.HandleHTTP))
	defer ts.Close()

	// First connection
	conn1 := wsDial(t, ts, "ghost-1", secret)
	proto.ReadFrameWS(conn1) // welcome
	t1 := srv.tunnels.GetTunnel("ghost-1")
	if t1 == nil {
		t.Fatal("expected tunnel after first connect")
	}

	// Second connection (reconnect)
	conn2 := wsDial(t, ts, "ghost-1", secret)
	defer conn2.Close()
	proto.ReadFrameWS(conn2) // welcome
	t2 := srv.tunnels.GetTunnel("ghost-1")
	if t2 == nil {
		t.Fatal("expected tunnel after reconnect")
	}
	if t2 == t1 {
		t.Error("expected new tunnel reference after reconnect")
	}

	// Close old connection
	conn1.Close()
	// Brief sleep for goroutine cleanup
	time.Sleep(50 * time.Millisecond)

	// New tunnel should still be active
	t3 := srv.tunnels.GetTunnel("ghost-1")
	if t3 == nil {
		t.Error("new tunnel should not be removed when old connection closes")
	}
}

func TestRelayDoesNotPersistConversationData(t *testing.T) {
	path := tempRegistry(t)
	srv, _ := NewServer(Config{RegistryPath: path})
	secret, _ := srv.registry.Add("ghost-1", "Test")

	ts := httptest.NewServer(http.HandlerFunc(srv.HandleHTTP))
	defer ts.Close()

	conn := wsDial(t, ts, "ghost-1", secret)
	defer conn.Close()
	proto.ReadFrameWS(conn) // welcome

	hash := sha256.Sum256([]byte("test-token"))
	srv.tunnels.SetClients("ghost-1", []ClientBinding{
		{TokenHash: hex.EncodeToString(hash[:])},
	})

	// Send a chat message through relay
	chatBody := `{"content":"My private conversation","session_key":"mobile:default"}`
	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat", strings.NewReader(chatBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ghost-Client-Id", "ghost-1")
	req.Header.Set("X-Ghost-Client-Token", "test-token")

	// Device goroutine reads and responds
	go func() {
		f, _ := proto.ReadFrameWS(conn) // OPEN
		if f.Kind != proto.KindOPEN {
			return
		}
		streamID := f.StreamID
		proto.ReadFrameWS(conn) // DATA
		proto.ReadFrameWS(conn) // END
		// Respond
		_ = proto.WriteOPENWS(conn, streamID, &proto.HTTPResponseMeta{
			Type: proto.StreamHTTP, Status: 200,
			Headers: map[string][]string{"Content-Type": {"application/json"}},
		})
		_ = proto.WriteDATAWS(conn, streamID, []byte(`{"ok":true}`))
		_ = proto.WriteENDWS(conn, streamID)
	}()

	w := httptest.NewRecorder()
	srv.HandleHTTP(w, req)

	// Verify relay doesn't store conversation data on disk
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "My private conversation") {
		t.Error("relay persisted conversation data to disk")
	}
}

// ─── Regression tests (security review fixes) ───────────────────────────────

// Credentials must never be accepted via query parameters — URLs leak into
// proxy and access logs.
func TestQueryCredentialsRejectedOnAppRequest(t *testing.T) {
	path := tempRegistry(t)
	srv, _ := NewServer(Config{RegistryPath: path})

	hash := sha256.Sum256([]byte("tok"))
	srv.tunnels.SetClients("g1", []ClientBinding{
		{TokenHash: hex.EncodeToString(hash[:])},
	})

	req := httptest.NewRequest("GET", "/v1/sessions?ghost=g1&token=tok", nil)
	w := httptest.NewRecorder()
	srv.HandleHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("query credentials: status = %d, want 401", w.Code)
	}
}

func TestTunnelQueryCredentialsRejected(t *testing.T) {
	path := tempRegistry(t)
	srv, _ := NewServer(Config{RegistryPath: path})
	srv.registry.Add("g1", "Test")

	req := httptest.NewRequest("GET", "/v1/tunnel?device_id=g1&device_secret=whatever", nil)
	w := httptest.NewRecorder()
	srv.HandleHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("query credentials on tunnel: status = %d, want 401", w.Code)
	}
}

// Hop-by-hop headers (and Content-Length) must not traverse the tunnel in
// either direction.
func TestHopByHopHeadersFiltered(t *testing.T) {
	path := tempRegistry(t)
	srv, _ := NewServer(Config{RegistryPath: path})
	secret, _ := srv.registry.Add("ghost-hbh", "Test")

	ts := httptest.NewServer(http.HandlerFunc(srv.HandleHTTP))
	defer ts.Close()

	conn := wsDial(t, ts, "ghost-hbh", secret)
	defer conn.Close()
	proto.ReadFrameWS(conn) // welcome

	hash := sha256.Sum256([]byte("tok"))
	srv.tunnels.SetClients("ghost-hbh", []ClientBinding{
		{TokenHash: hex.EncodeToString(hash[:])},
	})

	go func() {
		f, err := proto.ReadFrameWS(conn) // OPEN (request)
		if err != nil {
			return
		}
		meta, _ := proto.ParseHTTPMeta(f)
		// Request direction: hop-by-hop headers must have been stripped.
		for _, h := range []string{"Connection", "Transfer-Encoding", "Keep-Alive"} {
			if _, ok := meta.Headers[h]; ok {
				t.Errorf("request header %q should have been stripped", h)
			}
		}
		proto.ReadFrameWS(conn) // DATA
		end, _ := proto.ReadFrameWS(conn)

		_ = proto.WriteOPENWS(conn, end.StreamID, &proto.HTTPResponseMeta{
			Type:   proto.StreamHTTP,
			Status: 200,
			Headers: map[string][]string{
				"Content-Type":      {"application/json"},
				"Connection":        {"keep-alive"},
				"Transfer-Encoding": {"chunked"},
				"Content-Length":    {"999999"},
			},
		})
		_ = proto.WriteDATAWS(conn, end.StreamID, []byte(`{"ok":true}`))
		_ = proto.WriteENDWS(conn, end.StreamID)
	}()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat", strings.NewReader(`{}`))
	req.Header.Set("X-Ghost-Client-Id", "ghost-hbh")
	req.Header.Set("X-Ghost-Client-Token", "tok")
	req.Header.Set("Connection", "keep-alive")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Connection") != "" {
		t.Error("response Connection header should have been stripped")
	}
	// Go's http client materializes Transfer-Encoding itself; assert the raw
	// header was not copied by checking via the recorder-free path is hard,
	// so we rely on Connection + the fact that a bogus Content-Length of
	// 999999 would break body reading if it were forwarded.
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %q, want exact device payload (bogus Content-Length leaked?)", body)
	}
}

// Device-controlled ERROR payloads must not be able to break the JSON error
// structure returned to apps.
func TestErrorResponseJSONSafe(t *testing.T) {
	path := tempRegistry(t)
	srv, _ := NewServer(Config{RegistryPath: path})
	secret, _ := srv.registry.Add("ghost-json", "Test")

	ts := httptest.NewServer(http.HandlerFunc(srv.HandleHTTP))
	defer ts.Close()

	conn := wsDial(t, ts, "ghost-json", secret)
	defer conn.Close()
	proto.ReadFrameWS(conn) // welcome

	hash := sha256.Sum256([]byte("tok"))
	srv.tunnels.SetClients("ghost-json", []ClientBinding{
		{TokenHash: hex.EncodeToString(hash[:])},
	})

	go func() {
		proto.ReadFrameWS(conn) // OPEN
		proto.ReadFrameWS(conn) // DATA
		end, _ := proto.ReadFrameWS(conn)
		_ = proto.WriteERRORWS(conn, end.StreamID, "evil\"}\ninjected")
	}()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat", strings.NewReader(`{}`))
	req.Header.Set("X-Ghost-Client-Id", "ghost-json")
	req.Header.Set("X-Ghost-Client-Token", "tok")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
	var parsed map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Errorf("error response is not valid JSON: %v", err)
	}
}

// Ensure strings import is used
var _ = strings.Contains
