// Package native implements a browser automation engine in Go, speaking the
// Chrome DevTools Protocol directly. It is the Go reverse-engineering of the
// vercel-labs agent-browser daemon (cli/src/native): the same layered design —
// a websocket JSON-RPC client, a Chrome launcher, an accessibility-tree
// snapshot that mints durable @eN element refs, and ref-resolution into real
// input events — so Ghost can own its browser layer instead of shelling out to
// a third-party CLI.
//
// The package is deliberately a subset. It implements the operations the agent
// needs to perceive and act on a page (navigate, snapshot, click, fill, type,
// press, wait, screenshot, read) with the same ref discipline, and leaves the
// streaming/dashboard/recording surfaces of the upstream project out. The wire
// protocol is Chrome's, not the upstream project's, so nothing here depends on
// their code.
package native

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// cdpError is a protocol-level error returned in a command response.
type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *cdpError) Error() string { return fmt.Sprintf("cdp %d: %s", e.Code, e.Message) }

// cdpMessage is one frame in either direction.
type cdpMessage struct {
	ID     int64           `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *cdpError       `json:"error,omitempty"`
}

// cdpEvent is a protocol event delivered to a subscriber.
type cdpEvent struct {
	Method string
	Params json.RawMessage
}

// cdpClient is a single websocket connection to one CDP target (a page, or the
// browser endpoint). Commands are correlated by id; events are fanned out to
// method subscribers. One reader goroutine owns the socket; writes are
// serialized with a mutex, which is all CDP requires.
type cdpClient struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
	nextID  atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan cdpMessage
	subs    map[string][]chan cdpEvent
	closed  bool
	done    chan struct{}

	// lastErr records why the connection ended, surfaced to callers whose
	// pending command is abandoned by a disconnect.
	lastErr error
}

func dialCDP(ctx context.Context, wsURL string) (*cdpClient, error) {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", wsURL, err)
	}
	c := &cdpClient{
		conn:    conn,
		pending: map[int64]chan cdpMessage{},
		subs:    map[string][]chan cdpEvent{},
		done:    make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

func (c *cdpClient) readLoop() {
	defer func() {
		c.mu.Lock()
		c.closed = true
		if c.lastErr == nil {
			c.lastErr = fmt.Errorf("cdp connection closed")
		}
		for _, ch := range c.pending {
			close(ch)
		}
		c.pending = map[int64]chan cdpMessage{}
		for _, list := range c.subs {
			for _, ch := range list {
				close(ch)
			}
		}
		c.subs = map[string][]chan cdpEvent{}
		c.mu.Unlock()
		close(c.done)
	}()
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			c.mu.Lock()
			if c.lastErr == nil {
				c.lastErr = err
			}
			c.mu.Unlock()
			return
		}
		var m cdpMessage
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		if m.ID != 0 {
			c.mu.Lock()
			ch := c.pending[m.ID]
			delete(c.pending, m.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
				close(ch)
			}
			continue
		}
		if m.Method == "" {
			continue
		}
		c.mu.Lock()
		list := append([]chan cdpEvent(nil), c.subs[m.Method]...)
		c.mu.Unlock()
		ev := cdpEvent{Method: m.Method, Params: m.Params}
		for _, ch := range list {
			select {
			case ch <- ev:
			default: // a slow subscriber must never stall the reader
			}
		}
	}
}

// send runs one command and decodes its result into out (when non-nil).
func (c *cdpClient) send(ctx context.Context, method string, params interface{}, out interface{}) error {
	id := c.nextID.Add(1)
	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal %s params: %w", method, err)
		}
		rawParams = b
	}
	msg := cdpMessage{ID: id, Method: method, Params: rawParams}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	ch := make(chan cdpMessage, 1)
	c.mu.Lock()
	if c.closed {
		err := c.lastErr
		c.mu.Unlock()
		return fmt.Errorf("%s: %w", method, err)
	}
	c.pending[id] = ch
	c.mu.Unlock()

	c.writeMu.Lock()
	err = c.conn.WriteMessage(websocket.TextMessage, body)
	c.writeMu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("%s: write: %w", method, err)
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	case m, ok := <-ch:
		if !ok {
			c.mu.Lock()
			err := c.lastErr
			c.mu.Unlock()
			return fmt.Errorf("%s: %w", method, err)
		}
		if m.Error != nil {
			return fmt.Errorf("%s: %w", method, m.Error)
		}
		if out != nil && len(m.Result) > 0 {
			if err := json.Unmarshal(m.Result, out); err != nil {
				return fmt.Errorf("%s: decode result: %w", method, err)
			}
		}
		return nil
	}
}

// on subscribes to a protocol event. The returned channel is closed when the
// connection ends. Events are dropped rather than blocking when a subscriber
// falls behind.
func (c *cdpClient) on(method string) <-chan cdpEvent {
	ch := make(chan cdpEvent, 16)
	c.mu.Lock()
	if c.closed {
		close(ch)
		c.mu.Unlock()
		return ch
	}
	c.subs[method] = append(c.subs[method], ch)
	c.mu.Unlock()
	return ch
}

func (c *cdpClient) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	_ = c.conn.Close()
	<-c.done
}
