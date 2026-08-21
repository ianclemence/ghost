// Package server implements the Ghost relay server.
//
// The relay accepts persistent outbound WebSocket connections from Ghost
// devices and HTTP/WebSocket connections from paired mobile apps. It routes
// traffic between them using a binary multiplexed frame protocol.
//
// The relay is stateless for conversation content — it forwards bytes
// without inspecting or storing payloads.
package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ianclemence/ghost/pkg/relay/proto"
)

// RegistryEntry is a single device in the registry file.
type RegistryEntry struct {
	DeviceID     string `json:"device_id"`
	SecretHash   string `json:"secret_hash"` // hex(sha256(device_secret))
	DisplayName  string `json:"display_name,omitempty"`
	RegisteredAt string `json:"registered_at,omitempty"`
}

// Registry is the on-disk device registry.
type Registry struct {
	mu      sync.RWMutex
	path    string
	entries map[string]*RegistryEntry // keyed by device_id
}

// NewRegistry loads or creates a registry at path.
func NewRegistry(path string) (*Registry, error) {
	r := &Registry{
		path:    path,
		entries: make(map[string]*RegistryEntry),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return nil, fmt.Errorf("read registry: %w", err)
	}
	var list []RegistryEntry
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	for i := range list {
		r.entries[list[i].DeviceID] = &list[i]
	}
	return r, nil
}

// Add registers a device. Returns the plaintext secret (shown to operator once).
func (r *Registry) Add(deviceID, displayName string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entries[deviceID]; exists {
		return "", fmt.Errorf("device %s already registered", deviceID)
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	secretHex := hex.EncodeToString(secret)
	hash := sha256.Sum256([]byte(secretHex))

	entry := &RegistryEntry{
		DeviceID:     deviceID,
		SecretHash:   hex.EncodeToString(hash[:]),
		DisplayName:  displayName,
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	}
	r.entries[deviceID] = entry
	if err := r.save(); err != nil {
		delete(r.entries, deviceID)
		return "", err
	}
	return secretHex, nil
}

// Remove deletes a device from the registry.
func (r *Registry) Remove(deviceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[deviceID]; !exists {
		return fmt.Errorf("device %s not found", deviceID)
	}
	delete(r.entries, deviceID)
	return r.save()
}

// Authenticate checks device_id + secret against the registry.
// Comparison is constant-time.
func (r *Registry) Authenticate(deviceID, secret string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[deviceID]
	if !ok {
		return false
	}
	hash := sha256.Sum256([]byte(secret))
	return subtle.ConstantTimeCompare(
		[]byte(entry.SecretHash),
		[]byte(hex.EncodeToString(hash[:])),
	) == 1
}

// List returns all registered devices (without secrets).
func (r *Registry) List() []RegistryEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RegistryEntry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, *e)
	}
	return out
}

func (r *Registry) save() error {
	list := make([]RegistryEntry, 0, len(r.entries))
	for _, e := range r.entries {
		list = append(list, *e)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".registry-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, r.path)
}

// ClientBinding is a allowed client token bound to a device.
type ClientBinding struct {
	TokenHash string `json:"token_hash"`
	Name      string `json:"name,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

// TunnelManager manages active device tunnels.
type TunnelManager struct {
	mu         sync.RWMutex
	tunnels    map[string]*DeviceTunnel   // keyed by device_id
	clients    map[string][]ClientBinding // keyed by device_id
	generation map[string]uint64          // incremented on each RegisterTunnel
}

// NewTunnelManager creates a new tunnel manager.
func NewTunnelManager() *TunnelManager {
	return &TunnelManager{
		tunnels:    make(map[string]*DeviceTunnel),
		clients:    make(map[string][]ClientBinding),
		generation: make(map[string]uint64),
	}
}

// DeviceTunnel represents a single device's persistent tunnel connection.
type DeviceTunnel struct {
	DeviceID string
	Conn     *websocket.Conn
	Send     chan []byte
	Done     chan struct{}
	closeMu  sync.Once
}

// Close cleanly shuts down the tunnel.
func (t *DeviceTunnel) Close() {
	t.closeMu.Do(func() {
		close(t.Done)
		t.Conn.Close()
	})
}

// SendFrame queues a frame for delivery to the device. Safe to call from any
// goroutine; the tunnel's single write pump performs the actual socket write,
// guaranteeing no concurrent WebSocket writers.
func (t *DeviceTunnel) SendFrame(f *proto.Frame) error {
	data, err := proto.EncodeFrame(f)
	if err != nil {
		return err
	}
	select {
	case t.Send <- data:
		return nil
	case <-t.Done:
		return fmt.Errorf("tunnel closed")
	}
}

// RegisterTunnel stores a device tunnel, replacing any existing one. Returns the new generation.
func (tm *TunnelManager) RegisterTunnel(t *DeviceTunnel) uint64 {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if old, ok := tm.tunnels[t.DeviceID]; ok {
		old.Close()
	}
	tm.generation[t.DeviceID]++
	tm.tunnels[t.DeviceID] = t
	return tm.generation[t.DeviceID]
}

// RemoveIfOld removes the tunnel only if the generation matches (prevents stale goroutines from removing newer tunnels).
func (tm *TunnelManager) RemoveIfOld(deviceID string, gen uint64) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tm.generation[deviceID] != gen {
		return
	}
	if t, ok := tm.tunnels[deviceID]; ok {
		t.Close()
		delete(tm.tunnels, deviceID)
	}
	delete(tm.generation, deviceID)
}

// RemoveTunnel removes a device tunnel.
func (tm *TunnelManager) RemoveTunnel(deviceID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if t, ok := tm.tunnels[deviceID]; ok {
		t.Close()
		delete(tm.tunnels, deviceID)
	}
}

// GetTunnel returns the active tunnel for a device, or nil.
func (tm *TunnelManager) GetTunnel(deviceID string) *DeviceTunnel {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.tunnels[deviceID]
}

// SetClients replaces the allowed client list for a device.
func (tm *TunnelManager) SetClients(deviceID string, clients []ClientBinding) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.clients[deviceID] = clients
}

// GetClients returns the allowed client list for a device.
func (tm *TunnelManager) GetClients(deviceID string) []ClientBinding {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.clients[deviceID]
}

// AuthClient checks if a token is allowed for a device. tokenHex is the raw
// token presented by the app; we hash it and compare against stored hashes
// using a constant-time comparison.
func (tm *TunnelManager) AuthClient(deviceID, tokenHex string) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	clients, ok := tm.clients[deviceID]
	if !ok {
		return false
	}
	hash := sha256.Sum256([]byte(tokenHex))
	hashHex := hex.EncodeToString(hash[:])
	for _, c := range clients {
		if subtle.ConstantTimeCompare([]byte(c.TokenHash), []byte(hashHex)) == 1 {
			return true
		}
	}
	return false
}

// Config holds relay server configuration.
type Config struct {
	ListenAddr   string // e.g. ":8080" or "127.0.0.1:8080"
	TLSCertFile  string // empty = no TLS (dev mode)
	TLSKeyFile   string
	RegistryPath string // path to registry.json
	AdminSecret  string // optional admin token for enrollment
}

// Server is the relay server.
type Server struct {
	cfg      Config
	registry *Registry
	tunnels  *TunnelManager
	streams  *streamRegistry
	upgrader websocket.Upgrader
}

// NewServer creates a new relay server.
func NewServer(cfg Config) (*Server, error) {
	reg, err := NewRegistry(cfg.RegistryPath)
	if err != nil {
		return nil, fmt.Errorf("open registry: %w", err)
	}
	return &Server{
		cfg:      cfg,
		registry: reg,
		tunnels:  NewTunnelManager(),
		streams:  newStreamRegistry(),
		upgrader: websocket.Upgrader{
			CheckOrigin: checkSameOrigin,
		},
	}, nil
}

// checkSameOrigin allows non-browser clients (no Origin header) and
// same-origin browser clients. Cross-origin browser WebSocket connections
// are rejected — browsers are not a supported relay client in Phase 1.
func checkSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}

// hopByHopRequestHeaders are stripped from app requests before forwarding to
// the device. They describe connections, not content, and must not traverse
// the tunnel.
var hopByHopRequestHeaders = map[string]bool{
	"connection":          true,
	"keep-alive":          true,
	"proxy-connection":    true,
	"transfer-encoding":   true,
	"te":                  true,
	"trailer":             true,
	"upgrade":             true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"host":                true,
	// Relay auth headers are also stripped: the device-side relay client
	// injects X-Ghost-Secret locally; the relay never forwards credentials.
	"x-ghost-client-id":    true,
	"x-ghost-client-token": true,
	"x-ghost-secret":       true,
}

// hopByHopResponseHeaders are stripped from device responses before writing
// them to the app client. Content-Length is excluded because the relay
// re-chunks streamed DATA frames; a stale length would corrupt the response.
var hopByHopResponseHeaders = map[string]bool{
	"connection":          true,
	"keep-alive":          true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"transfer-encoding":   true,
	"te":                  true,
	"trailer":             true,
	"upgrade":             true,
	"content-length":      true,
}

// HandleHTTP is the main HTTP handler for app-facing endpoints.
func (s *Server) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case path == "/v1/health":
		s.handleHealth(w, r)
	case path == "/v1/enroll":
		s.handleEnroll(w, r)
	case path == "/v1/tunnel" || strings.HasPrefix(path, "/v1/tunnel"):
		s.handleDeviceTunnel(w, r)
	case strings.HasPrefix(path, "/v1/"):
		s.handleAppRequest(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"version": proto.Version,
	})
}

// handleEnroll allows a device to register itself with an admin token.
func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.AdminSecret == "" {
		http.Error(w, `{"error":"enrollment disabled"}`, http.StatusForbidden)
		return
	}

	var req struct {
		DeviceID     string `json:"device_id"`
		DeviceSecret string `json:"device_secret"`
		AdminToken   string `json:"admin_token"`
		Name         string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}
	adminHash := sha256.Sum256([]byte(req.AdminToken))
	wantHash := sha256.Sum256([]byte(s.cfg.AdminSecret))
	if subtle.ConstantTimeCompare(adminHash[:], wantHash[:]) != 1 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if req.DeviceID == "" || req.DeviceSecret == "" {
		http.Error(w, `{"error":"device_id and device_secret required"}`, http.StatusBadRequest)
		return
	}

	hash := sha256.Sum256([]byte(req.DeviceSecret))
	entry := &RegistryEntry{
		DeviceID:     req.DeviceID,
		SecretHash:   hex.EncodeToString(hash[:]),
		DisplayName:  req.Name,
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	}
	s.registry.mu.Lock()
	s.registry.entries[req.DeviceID] = entry
	err := s.registry.save()
	s.registry.mu.Unlock()

	if err != nil {
		http.Error(w, `{"error":"failed to save"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// handleDeviceTunnel accepts a WebSocket connection from a Ghost device.
// Credentials are accepted ONLY via headers — never query parameters, which
// leak into proxy and access logs.
func (s *Server) handleDeviceTunnel(w http.ResponseWriter, r *http.Request) {
	deviceID := r.Header.Get("X-Ghost-Device")
	deviceSecret := r.Header.Get("X-Ghost-Device-Secret")
	if deviceID == "" || deviceSecret == "" {
		http.Error(w, `{"error":"device credentials required"}`, http.StatusUnauthorized)
		return
	}
	if !s.registry.Authenticate(deviceID, deviceSecret) {
		http.Error(w, `{"error":"invalid device credentials"}`, http.StatusUnauthorized)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("relay: tunnel upgrade failed: %v", err)
		return
	}

	tunnel := &DeviceTunnel{
		DeviceID: deviceID,
		Conn:     conn,
		Send:     make(chan []byte, 256),
		Done:     make(chan struct{}),
	}
	gen := s.tunnels.RegisterTunnel(tunnel)
	log.Printf("relay: device %s connected", deviceID)

	// Send welcome
	_ = proto.WriteCTLWS(conn, 0, &proto.Control{
		Op:      proto.OpWelcome,
		Version: proto.Version,
	})

	// Read loop: device → relay (control frames + stream data)
	go s.readDeviceLoop(tunnel, gen)

	// Write pump: relay → device (single writer)
	go s.writePump(tunnel)
}

// readDeviceLoop processes frames from the device tunnel.
func (s *Server) readDeviceLoop(t *DeviceTunnel, gen uint64) {
	defer func() {
		s.tunnels.RemoveIfOld(t.DeviceID, gen)
		log.Printf("relay: device %s disconnected", t.DeviceID)
	}()

	for {
		select {
		case <-t.Done:
			return
		default:
		}
		f, err := proto.ReadFrameWS(t.Conn)
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) &&
				!isEOF(err) {
				log.Printf("relay: read frame from %s: %v", t.DeviceID, err)
			}
			return
		}

		switch f.Kind {
		case proto.KindCTL:
			s.handleDeviceControl(t, f)
		case proto.KindPING:
			_ = t.Conn.WriteControl(websocket.PongMessage, nil, time.Now().Add(5*time.Second))
		case proto.KindPONG:
			// heartbeat acknowledged
		default:
			// DATA/END/ERROR/OPEN for streams managed by app request handlers
			s.dispatchToDeviceStream(t, f)
		}
	}
}

// handleDeviceControl processes control messages from the device.
func (s *Server) handleDeviceControl(t *DeviceTunnel, f *proto.Frame) {
	ctrl, err := proto.ParseControl(f)
	if err != nil {
		log.Printf("relay: bad control from %s: %v", t.DeviceID, err)
		return
	}

	switch ctrl.Op {
	case proto.OpAddClients:
		bindings := make([]ClientBinding, len(ctrl.Clients))
		for i, c := range ctrl.Clients {
			bindings[i] = ClientBinding{TokenHash: c.TokenHash, Name: c.Name}
		}
		s.tunnels.SetClients(t.DeviceID, bindings)
		_ = proto.WriteCTLWS(t.Conn, 0, &proto.Control{
			Op: proto.OpClientsOK,
			OK: true,
		})
		log.Printf("relay: device %s updated %d client bindings", t.DeviceID, len(ctrl.Clients))

	case proto.OpRevokeClient:
		clients := s.tunnels.GetClients(t.DeviceID)
		filtered := make([]ClientBinding, 0, len(clients))
		for _, c := range clients {
			if c.TokenHash != ctrl.TokenHash {
				filtered = append(filtered, c)
			}
		}
		s.tunnels.SetClients(t.DeviceID, filtered)
		_ = proto.WriteCTLWS(t.Conn, 0, &proto.Control{
			Op: proto.OpClientsOK,
			OK: true,
		})

	case proto.OpHeartbeat:
		_ = proto.WriteCTLWS(t.Conn, 0, &proto.Control{
			Op: proto.OpHeartbeat,
			OK: true,
		})

	default:
		log.Printf("relay: unknown control op %q from %s", ctrl.Op, t.DeviceID)
	}
}

// writePump is the single writer for the tunnel connection. Data frames are
// consumed from t.Send; ping frames use WriteControl, which gorilla permits
// concurrently with data writes.
func (s *Server) writePump(t *DeviceTunnel) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-t.Done:
			return
		case msg := <-t.Send:
			_ = t.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := t.Conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
				log.Printf("relay: write to %s: %v", t.DeviceID, err)
				return
			}
		case <-ticker.C:
			if err := t.Conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				return
			}
		}
	}
}

// streamState tracks a single multiplexed stream for an app request.
type streamState struct {
	id        uint64
	tunnel    *DeviceTunnel
	ch        chan *proto.Frame
	done      chan struct{}
	closeOnce sync.Once
}

func (s *streamState) close() {
	s.closeOnce.Do(func() { close(s.done) })
}

// streamRegistry manages active streams keyed by "deviceID:streamID".
type streamRegistry struct {
	mu      sync.Mutex
	streams map[string]*streamState
	nextID  uint64
}

func newStreamRegistry() *streamRegistry {
	return &streamRegistry{
		streams: make(map[string]*streamState),
		nextID:  1,
	}
}

func (sr *streamRegistry) nextStreamID() uint64 {
	id := sr.nextID
	sr.nextID += 2
	return id
}

func (sr *streamRegistry) register(key string, s *streamState) {
	sr.mu.Lock()
	sr.streams[key] = s
	sr.mu.Unlock()
}

func (sr *streamRegistry) get(key string) *streamState {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	return sr.streams[key]
}

func (sr *streamRegistry) remove(key string) {
	sr.mu.Lock()
	delete(sr.streams, key)
	sr.mu.Unlock()
}

// dispatchToDeviceStream routes device frames to the waiting stream handler.
// Frames for unknown streams are dropped — a device can only reach streams
// under its own device ID prefix, so cross-device injection is impossible.
func (s *Server) dispatchToDeviceStream(t *DeviceTunnel, f *proto.Frame) {
	key := fmt.Sprintf("%s:%d", t.DeviceID, f.StreamID)
	st := s.streams.get(key)
	if st == nil {
		return
	}
	select {
	case st.ch <- f:
	case <-st.done:
	}
}

// handleAppRequest forwards an HTTP request from the app to the device tunnel.
// App credentials are accepted ONLY via headers — never query parameters.
func (s *Server) handleAppRequest(w http.ResponseWriter, r *http.Request) {
	// Authenticate app via client token
	deviceID := r.Header.Get("X-Ghost-Client-Id")
	clientToken := r.Header.Get("X-Ghost-Client-Token")
	if deviceID == "" || clientToken == "" {
		http.Error(w, `{"error":"ghost and token required"}`, http.StatusUnauthorized)
		return
	}
	if !s.tunnels.AuthClient(deviceID, clientToken) {
		http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
		return
	}

	// Find device tunnel
	tunnel := s.tunnels.GetTunnel(deviceID)
	if tunnel == nil {
		http.Error(w, `{"error":"device offline"}`, http.StatusServiceUnavailable)
		return
	}

	// Read request body
	var body []byte
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB max
		if err != nil {
			http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
			return
		}
	}

	// Copy headers, stripping hop-by-hop headers and relay credentials.
	headers := make(map[string][]string)
	for k, v := range r.Header {
		if hopByHopRequestHeaders[strings.ToLower(k)] {
			continue
		}
		headers[k] = v
	}

	// Allocate stream ID
	streamID := s.streams.nextStreamID()
	key := fmt.Sprintf("%s:%d", deviceID, streamID)

	st := &streamState{
		id:     streamID,
		tunnel: tunnel,
		ch:     make(chan *proto.Frame, 64),
		done:   make(chan struct{}),
	}
	s.streams.register(key, st)
	defer s.streams.remove(key)
	defer st.close()

	// Send OPEN frame to device via the tunnel's single-writer pump
	metaBytes, err := json.Marshal(&proto.HTTPMetadata{
		Type:    proto.StreamHTTP,
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: headers,
	})
	if err != nil {
		http.Error(w, `{"error":"encode request"}`, http.StatusInternalServerError)
		return
	}
	if err := tunnel.SendFrame(&proto.Frame{Kind: proto.KindOPEN, StreamID: streamID, Payload: metaBytes}); err != nil {
		log.Printf("relay: send open to %s: %v", deviceID, err)
		http.Error(w, `{"error":"tunnel write"}`, http.StatusBadGateway)
		return
	}

	// Send request body as DATA frame, then END
	if len(body) > 0 {
		if err := tunnel.SendFrame(&proto.Frame{Kind: proto.KindDATA, StreamID: streamID, Payload: body}); err != nil {
			http.Error(w, `{"error":"tunnel write body"}`, http.StatusBadGateway)
			return
		}
	}
	if err := tunnel.SendFrame(&proto.Frame{Kind: proto.KindEND, StreamID: streamID}); err != nil {
		http.Error(w, `{"error":"tunnel write end"}`, http.StatusBadGateway)
		return
	}

	// Wait for response OPEN frame from device
	timeout := time.After(30 * time.Second)
	var respMeta *proto.HTTPResponseMeta
loop:
	for {
		select {
		case <-timeout:
			http.Error(w, `{"error":"device timeout"}`, http.StatusGatewayTimeout)
			return
		case f := <-st.ch:
			switch f.Kind {
			case proto.KindOPEN:
				meta, err := proto.ParseHTTPResponseMeta(f)
				if err != nil {
					http.Error(w, `{"error":"bad response meta"}`, http.StatusBadGateway)
					return
				}
				respMeta = meta
				break loop
			case proto.KindERROR:
				// JSON-encode the message so device-controlled payloads
				// cannot break out of the JSON structure.
				msg, _ := json.Marshal(truncate(string(f.Payload), 200))
				http.Error(w, fmt.Sprintf(`{"error":%s}`, msg), http.StatusBadGateway)
				return
			case proto.KindEND:
				// Device closed without sending response
				http.Error(w, `{"error":"device closed"}`, http.StatusBadGateway)
				return
			}
		case <-st.done:
			http.Error(w, `{"error":"stream closed"}`, http.StatusBadGateway)
			return
		}
	}

	// Write response headers — preserve device's Content-Type (critical for
	// SSE) but strip hop-by-hop headers and Content-Length (the relay
	// re-chunks the stream).
	for k, vv := range respMeta.Headers {
		if hopByHopResponseHeaders[strings.ToLower(k)] {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("X-Ghost-Relay", "true")
	w.WriteHeader(respMeta.Status)

	// Stream response body from device DATA frames
	flusher, ok := w.(http.Flusher)
	for {
		select {
		case <-st.done:
			return
		case f := <-st.ch:
			switch f.Kind {
			case proto.KindDATA:
				w.Write(f.Payload)
				if ok {
					flusher.Flush()
				}
			case proto.KindEND:
				return
			case proto.KindERROR:
				log.Printf("relay: stream error from %s: %.200s", deviceID, truncate(string(f.Payload), 200))
				return
			}
		}
	}
}

// truncate limits a string length for logging/error passthrough.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Serve starts the relay server.
func (s *Server) Serve() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.HandleHTTP)

	server := &http.Server{
		Addr:              s.cfg.ListenAddr,
		Handler:           mux,
		ReadTimeout:       60 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		WriteTimeout:      0, // streaming
	}

	if s.cfg.TLSCertFile != "" && s.cfg.TLSKeyFile != "" {
		log.Printf("relay: listening on %s (TLS)", s.cfg.ListenAddr)
		return server.ListenAndServeTLS(s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
	}
	log.Printf("relay: listening on %s (no TLS)", s.cfg.ListenAddr)
	return server.ListenAndServe()
}

// GenerateToken generates a random 32-byte hex token for client pairing.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// HashToken returns the SHA-256 hash of a token (hex-encoded).
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func isEOF(err error) bool {
	return err == io.EOF || err == io.ErrUnexpectedEOF
}
