// Package relayclient implements the Ghost device-side relay client.
//
// It maintains an outbound WebSocket tunnel to the relay server and forwards
// HTTP requests from paired apps to the local Ghost gateway via localhost.
//
// Concurrency model: readLoop is the SOLE reader on the WebSocket. Frames for
// individual streams are routed to per-stream handler goroutines via channels.
// All data writes go through a single write pump channel; control frames use
// WriteControl, which is safe concurrently.
package relayclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ianclemence/ghost/pkg/relay/proto"
)

// ClientConfig holds the relay client configuration.
type ClientConfig struct {
	DeviceID     string // ghost_id, used as device identifier
	DeviceSecret string // loaded from .secrets.json
	RelayServer  string // e.g. wss://relay.example.com or ws://127.0.0.1:8080
	GatewayURL   string // e.g. http://127.0.0.1:8766 (local gateway)
	ReconnectMin int    // minimum reconnect delay in seconds
	ReconnectMax int    // maximum reconnect delay in seconds
}

// Client is the relay client.
type Client struct {
	cfg  ClientConfig
	send chan []byte
	done chan struct{}

	connMu     sync.Mutex
	conn       *websocket.Conn // active connection; closed by Stop()
	connDoneMu sync.Mutex
	connDone   chan struct{} // per-connection lifecycle; closed when the active connection ends
}

// NewClient creates a new relay client.
func NewClient(cfg ClientConfig) *Client {
	if cfg.ReconnectMin <= 0 {
		cfg.ReconnectMin = 1
	}
	if cfg.ReconnectMax <= 0 {
		cfg.ReconnectMax = 60
	}
	if cfg.GatewayURL == "" {
		cfg.GatewayURL = "http://127.0.0.1:8766"
	}
	return &Client{
		cfg:  cfg,
		send: make(chan []byte, 256),
		done: make(chan struct{}),
	}
}

func (c *Client) setConnDone(d chan struct{}) {
	c.connDoneMu.Lock()
	c.connDone = d
	c.connDoneMu.Unlock()
}

func (c *Client) getConnDone() chan struct{} {
	c.connDoneMu.Lock()
	defer c.connDoneMu.Unlock()
	return c.connDone
}

// sendFrame queues a frame for delivery to the relay via the write pump.
// Safe from any goroutine; unblocks if the client stops or the current
// connection dies.
func (c *Client) sendFrame(f *proto.Frame) error {
	data, err := proto.EncodeFrame(f)
	if err != nil {
		return err
	}
	select {
	case c.send <- data:
		return nil
	case <-c.done:
		return fmt.Errorf("client stopped")
	case <-c.getConnDone():
		return fmt.Errorf("connection closed")
	}
}

// Run connects to the relay and maintains the tunnel. Blocks until ctx is cancelled.
func (c *Client) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	backoff := c.cfg.ReconnectMin
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := c.connectAndRun(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("relay-client: disconnected: %v, reconnecting in %ds", err, backoff)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(backoff) * time.Second):
		}

		backoff = backoff * 2
		if backoff > c.cfg.ReconnectMax {
			backoff = c.cfg.ReconnectMax
		}
	}
}

// connectAndRun establishes a single tunnel connection and processes frames.
func (c *Client) connectAndRun(ctx context.Context) error {
	// Build WebSocket URL. Credentials are sent ONLY as handshake headers —
	// never query parameters, which leak into proxy and access logs.
	u, err := url.Parse(c.cfg.RelayServer)
	if err != nil {
		return fmt.Errorf("parse relay URL: %w", err)
	}
	switch u.Scheme {
	case "ws", "wss":
		// ok
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = "/v1/tunnel"
	u.RawQuery = ""

	log.Printf("relay-client: connecting to %s", u.String())

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}
	conn, _, err := dialer.DialContext(ctx, u.String(), http.Header{
		"X-Ghost-Device":        []string{c.cfg.DeviceID},
		"X-Ghost-Device-Secret": []string{c.cfg.DeviceSecret},
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()

	// Per-connection lifecycle: closed when this connection ends so that
	// pending sendFrame calls and stream handlers unblock promptly.
	connDone := make(chan struct{})
	var closeConnOnce sync.Once
	defer closeConnOnce.Do(func() { close(connDone) })
	c.setConnDone(connDone)

	// Read welcome
	f, err := proto.ReadFrameWS(conn)
	if err != nil {
		return fmt.Errorf("read welcome: %w", err)
	}
	if f.Kind != proto.KindCTL {
		return fmt.Errorf("expected welcome CTL, got kind=%d", f.Kind)
	}
	ctrl, err := proto.ParseControl(f)
	if err != nil {
		return fmt.Errorf("parse welcome: %w", err)
	}
	if ctrl.Op != proto.OpWelcome {
		return fmt.Errorf("expected welcome, got op=%s", ctrl.Op)
	}
	log.Printf("relay-client: connected, relay version=%d", ctrl.Version)

	// Send clients list (device is source of truth)
	clients, err := loadClients(c.cfg.DeviceID)
	if err != nil {
		log.Printf("relay-client: load clients: %v", err)
	} else if len(clients) > 0 {
		entries := make([]proto.ClientEntry, len(clients))
		for i, cl := range clients {
			entries[i] = proto.ClientEntry{TokenHash: cl.TokenHash, Name: cl.Name}
		}
		_ = proto.WriteCTLWS(conn, 0, &proto.Control{
			Op:      proto.OpAddClients,
			Clients: entries,
		})
	}

	// Write pump (single writer for data frames)
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		c.writePump(conn, connDone)
	}()

	// Read loop (sole reader). Returns when the connection dies or ctx cancels.
	readErr := c.readLoop(ctx, conn, connDone)

	// Wait for write pump to drain or give up
	select {
	case <-writeDone:
	case <-time.After(5 * time.Second):
	}

	c.connMu.Lock()
	if c.conn == conn {
		c.conn = nil
	}
	c.connMu.Unlock()

	return readErr
}

// streamHandler tracks one in-flight forwarded request.
type streamHandler struct {
	ch      chan *proto.Frame
	closeCh sync.Once
}

func (sh *streamHandler) close() {
	sh.closeCh.Do(func() { close(sh.ch) })
}

// readLoop processes ALL inbound frames from the relay. OPEN frames spawn a
// stream handler; DATA/END/ERROR frames are routed to the matching handler's
// channel. This guarantees a single reader on the WebSocket connection.
func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn, connDone chan struct{}) error {
	streams := make(map[uint64]*streamHandler)
	var streamsMu sync.Mutex

	defer func() {
		// Close all stream channels so handlers abort promptly on disconnect.
		streamsMu.Lock()
		for _, sh := range streams {
			sh.close()
		}
		streams = make(map[uint64]*streamHandler)
		streamsMu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-connDone:
			return fmt.Errorf("connection closed")
		default:
		}

		f, err := proto.ReadFrameWS(conn)
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) ||
				err == io.EOF {
				return fmt.Errorf("connection closed: %w", err)
			}
			return fmt.Errorf("read frame: %w", err)
		}

		switch f.Kind {
		case proto.KindCTL:
			c.handleControl(f)
		case proto.KindOPEN:
			// New stream from relay (HTTP request to forward)
			sh := &streamHandler{ch: make(chan *proto.Frame, 64)}
			streamsMu.Lock()
			streams[f.StreamID] = sh
			streamsMu.Unlock()
			go c.handleStream(sh, f)
		case proto.KindDATA, proto.KindEND, proto.KindERROR:
			streamsMu.Lock()
			sh, ok := streams[f.StreamID]
			streamsMu.Unlock()
			if !ok {
				continue // unknown/stale stream — drop
			}
			select {
			case sh.ch <- f:
			case <-time.After(30 * time.Second):
				// Handler stalled too long; abandon it.
				sh.close()
				streamsMu.Lock()
				delete(streams, f.StreamID)
				streamsMu.Unlock()
			}
			if f.Kind == proto.KindEND || f.Kind == proto.KindERROR {
				streamsMu.Lock()
				delete(streams, f.StreamID)
				streamsMu.Unlock()
				sh.close()
			}
		case proto.KindPING:
			_ = conn.WriteControl(websocket.PongMessage, nil, time.Now().Add(5*time.Second))
		case proto.KindPONG:
			// heartbeat ack
		default:
			log.Printf("relay-client: unexpected frame kind=%d stream=%d", f.Kind, f.StreamID)
		}
	}
}

// handleControl processes control messages from the relay.
func (c *Client) handleControl(f *proto.Frame) {
	ctrl, err := proto.ParseControl(f)
	if err != nil {
		log.Printf("relay-client: bad control: %v", err)
		return
	}
	switch ctrl.Op {
	case proto.OpClientsOK:
		log.Printf("relay-client: client bindings confirmed")
	case proto.OpError:
		log.Printf("relay-client: relay error: %s", ctrl.Message)
	default:
		log.Printf("relay-client: unknown control op: %s", ctrl.Op)
	}
}

// handleStream processes a single multiplexed stream (HTTP request forwarding).
// It reads exclusively from the stream's channel — never from the WebSocket.
func (c *Client) handleStream(sh *streamHandler, openFrame *proto.Frame) {
	streamID := openFrame.StreamID

	meta, err := proto.ParseHTTPMeta(openFrame)
	if err != nil {
		log.Printf("relay-client: bad open metadata: %v", err)
		_ = c.sendFrame(&proto.Frame{Kind: proto.KindERROR, StreamID: streamID, Payload: []byte("bad open metadata")})
		return
	}

	if meta.Type != proto.StreamHTTP {
		_ = c.sendFrame(&proto.Frame{Kind: proto.KindERROR, StreamID: streamID, Payload: []byte("unsupported stream type")})
		return
	}

	// Read request body from the stream channel until END
	var body []byte
loop:
	for f := range sh.ch {
		switch f.Kind {
		case proto.KindDATA:
			body = append(body, f.Payload...)
		case proto.KindEND:
			break loop
		case proto.KindERROR:
			log.Printf("relay-client: stream %d error: %.200s", streamID, truncateStr(string(f.Payload), 200))
			return
		default:
			log.Printf("relay-client: unexpected frame kind=%d in stream %d", f.Kind, streamID)
		}
	}

	// Build local HTTP request against the local gateway only.
	reqURL := c.cfg.GatewayURL + meta.Path
	req, err := http.NewRequest(meta.Method, reqURL, bytes.NewReader(body))
	if err != nil {
		_ = c.sendFrame(&proto.Frame{Kind: proto.KindERROR, StreamID: streamID, Payload: []byte("bad request")})
		return
	}

	// Copy headers from the original request
	for k, vv := range meta.Headers {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}

	// Mark the request as relay-forwarded for gateway-side auditability.
	req.Header.Set("X-Ghost-Via", "relay")

	// Execute request against local gateway
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		// Log full detail locally; send a generic message through the relay
		// so internal URLs never traverse the tunnel.
		log.Printf("relay-client: gateway error (stream %d): %v", streamID, err)
		_ = c.sendFrame(&proto.Frame{Kind: proto.KindERROR, StreamID: streamID, Payload: []byte("gateway request failed")})
		return
	}
	defer resp.Body.Close()

	// Send response OPEN
	respHeaders := make(map[string][]string)
	for k, vv := range resp.Header {
		respHeaders[k] = vv
	}
	respMetaBytes, err := json.Marshal(&proto.HTTPResponseMeta{
		Type:    proto.StreamHTTP,
		Status:  resp.StatusCode,
		Headers: respHeaders,
	})
	if err != nil {
		_ = c.sendFrame(&proto.Frame{Kind: proto.KindERROR, StreamID: streamID, Payload: []byte("encode response")})
		return
	}
	if err := c.sendFrame(&proto.Frame{Kind: proto.KindOPEN, StreamID: streamID, Payload: respMetaBytes}); err != nil {
		return
	}

	// Stream response body
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if writeErr := c.sendFrame(&proto.Frame{Kind: proto.KindDATA, StreamID: streamID, Payload: chunk}); writeErr != nil {
				return
			}
		}
		if readErr != nil {
			break
		}
	}

	// Signal end of stream
	_ = c.sendFrame(&proto.Frame{Kind: proto.KindEND, StreamID: streamID})
}

// writePump is the single writer for data frames. Pings use WriteControl,
// which gorilla permits concurrently with data writes.
func (c *Client) writePump(conn *websocket.Conn, connDone chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-connDone:
			return
		case msg := <-c.send:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				return
			}
		}
	}
}

// Stop cleanly shuts down the client. Closing the connection unblocks the
// read loop, which in turn unblocks stream handlers and the write pump.
func (c *Client) Stop() {
	close(c.done)
	c.connMu.Lock()
	if c.conn != nil {
		c.conn.Close()
	}
	c.connMu.Unlock()
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// --- Client token management (device-side, persisted locally) ---

// StoredClient is a client token stored on the device.
type StoredClient struct {
	TokenHash string `json:"token_hash"`
	Token     string `json:"token,omitempty"` // raw token, shown once during pairing
	Name      string `json:"name,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

func clientsPath(deviceID string) string {
	home, _ := os.UserHomeDir()
	ghostDir := os.Getenv("GHOST_DIR")
	if ghostDir == "" {
		ghostDir = filepath.Join(home, "ghost")
	}
	return filepath.Join(ghostDir, "workspace", "state", "relay_clients.json")
}

// loadClients loads the client list from disk.
func loadClients(deviceID string) ([]StoredClient, error) {
	path := clientsPath(deviceID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var clients []StoredClient
	if err := json.Unmarshal(data, &clients); err != nil {
		return nil, err
	}
	return clients, nil
}

// SaveClients persists the client list to disk (0600).
func SaveClients(deviceID string, clients []StoredClient) error {
	path := clientsPath(deviceID)
	data, err := json.MarshalIndent(clients, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".relay-clients-*")
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
	return os.Rename(tmpName, path)
}

// AddClient generates a new client token, stores it, and returns the raw token.
func AddClient(deviceID, name string) (string, error) {
	clients, err := loadClients(deviceID)
	if err != nil {
		return "", err
	}

	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	tokenHex := hex.EncodeToString(token)
	h := sha256.Sum256([]byte(tokenHex))

	clients = append(clients, StoredClient{
		TokenHash: hex.EncodeToString(h[:]),
		Token:     tokenHex,
		Name:      name,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	if err := SaveClients(deviceID, clients); err != nil {
		return "", err
	}
	return tokenHex, nil
}

// RemoveClient removes a client by token hash prefix (first 16 chars).
func RemoveClient(deviceID, tokenHashPrefix string) error {
	clients, err := loadClients(deviceID)
	if err != nil {
		return err
	}
	filtered := make([]StoredClient, 0, len(clients))
	found := false
	for _, c := range clients {
		if len(c.TokenHash) >= len(tokenHashPrefix) && c.TokenHash[:len(tokenHashPrefix)] == tokenHashPrefix {
			found = true
			continue
		}
		filtered = append(filtered, c)
	}
	if !found {
		return fmt.Errorf("client not found")
	}
	return SaveClients(deviceID, filtered)
}

// ListClients returns all stored clients (with token hashes, without raw tokens).
func ListClients(deviceID string) ([]StoredClient, error) {
	return loadClients(deviceID)
}

// GenerateToken generates a random 32-byte hex token (64 hex chars).
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
