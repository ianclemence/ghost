package relayclient

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/relay/proto"
	"github.com/ianclemence/ghost/pkg/relaycrypto"
)

// sealedWindow is how far a sealed request's timestamp may be from the Pod's
// clock. Phones drift; five minutes forgives that and still keeps the replay
// memory small.
const sealedWindow = 5 * time.Minute

// SealedRequest is what the phone seals: the request it wants made, and the
// token that proves it is a paired client. Both travel inside the envelope, so
// the relay learns neither the path nor the credentials.
type SealedRequest struct {
	Timestamp int64               `json:"ts"` // unix milliseconds
	Method    string              `json:"method"`
	Path      string              `json:"path"`
	Query     string              `json:"query,omitempty"`
	Headers   map[string][]string `json:"headers,omitempty"`
	Body      string              `json:"body,omitempty"` // base64
	Token     string              `json:"token"`
}

// SealedHead is the first sealed message of a response.
type SealedHead struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers,omitempty"`
}

// sealedBlocked headers never come from a relayed request: they describe the
// connection or would let a request claim to be something it is not.
var sealedBlocked = map[string]bool{
	"host": true, "connection": true, "keep-alive": true, "transfer-encoding": true,
	"te": true, "trailer": true, "upgrade": true, "proxy-authorization": true,
	"proxy-authenticate": true, "proxy-connection": true, "content-length": true,
	"x-ghost-via": true, "x-ghost-client-id": true, "x-ghost-client-token": true,
	"x-ghost-secret": true, "x-forwarded-for": true,
}

// handleSealed answers one sealed exchange. Anything wrong with the envelope is
// answered with a stream error and no detail: an attacker learns nothing from
// the difference between a stale stamp, a replay and a bad token.
func (c *Client) handleSealed(sh *streamHandler, streamID uint64) {
	fail := func(why string) {
		log.Printf("relay-client: sealed request refused (%s)", why)
		_ = c.sendFrame(&proto.Frame{Kind: proto.KindERROR, StreamID: streamID, Payload: []byte("refused")})
	}
	if c.cfg.Identity == nil {
		fail("no identity key")
		return
	}

	var env []byte
loop:
	for f := range sh.ch {
		switch f.Kind {
		case proto.KindDATA:
			env = append(env, f.Payload...)
			if len(env) > relaycrypto.MaxRequest {
				fail("too large")
				return
			}
		case proto.KindEND:
			break loop
		case proto.KindERROR:
			return
		}
	}

	sess, plain, eph, err := c.cfg.Identity.OpenRequest(env)
	if err != nil {
		fail("did not open")
		return
	}
	var req SealedRequest
	if err := json.Unmarshal(plain, &req); err != nil {
		fail("bad request")
		return
	}
	now := time.Now()
	if d := now.Sub(time.UnixMilli(req.Timestamp)); d > c.guard.Window() || d < -c.guard.Window() {
		fail("stale")
		return
	}
	if !c.guard.Fresh(eph, now) {
		fail("replay")
		return
	}

	scope, ok := c.authorize(req.Token)
	if !ok {
		fail("unknown client")
		return
	}
	if !strings.HasPrefix(req.Path, "/v1/") || strings.Contains(req.Path, "..") ||
		!proto.ScopeAllows(scope, req.Method, req.Path) {
		fail("not allowed")
		return
	}
	body, err := base64.StdEncoding.DecodeString(req.Body)
	if err != nil {
		fail("bad body")
		return
	}

	// From here the request is the owner's and the Pod answers it. The relay is
	// told the exchange is open and then sees only sealed frames.
	openMeta, _ := json.Marshal(&proto.HTTPResponseMeta{Type: proto.StreamSealed, Status: http.StatusOK})
	if err := c.sendFrame(&proto.Frame{Kind: proto.KindOPEN, StreamID: streamID, Payload: openMeta}); err != nil {
		return
	}
	send := func(kind byte, content []byte) error {
		m, err := sess.SealMessage(kind, content)
		if err != nil {
			return err
		}
		return c.sendFrame(&proto.Frame{Kind: proto.KindDATA, StreamID: streamID, Payload: relaycrypto.Frame(m)})
	}
	sendErr := func(why string) {
		_ = send(relaycrypto.MsgErr, []byte(why))
		_ = c.sendFrame(&proto.Frame{Kind: proto.KindEND, StreamID: streamID})
	}

	reqURL := c.cfg.GatewayURL + req.Path
	if req.Query != "" {
		reqURL += "?" + req.Query
	}
	hreq, err := http.NewRequest(req.Method, reqURL, bytes.NewReader(body))
	if err != nil {
		sendErr("bad request")
		return
	}
	for k, vv := range req.Headers {
		if sealedBlocked[strings.ToLower(k)] {
			continue
		}
		for _, v := range vv {
			hreq.Header.Add(k, v)
		}
	}
	// Set after the copy so the request cannot clear it: relayed requests must
	// present device credentials like any other remote peer.
	hreq.Header.Set("X-Ghost-Via", "relay")

	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(hreq)
	if err != nil {
		log.Printf("relay-client: gateway error (stream %d): %v", streamID, err)
		sendErr("gateway request failed")
		return
	}
	defer resp.Body.Close()

	head, _ := json.Marshal(&SealedHead{Status: resp.StatusCode, Headers: resp.Header})
	if err := send(relaycrypto.MsgHead, head); err != nil {
		return
	}
	buf := make([]byte, 32*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if err := send(relaycrypto.MsgData, buf[:n]); err != nil {
				return
			}
		}
		if rerr != nil {
			if rerr != io.EOF {
				sendErr("response interrupted")
				return
			}
			break
		}
	}
	if err := send(relaycrypto.MsgEnd, nil); err != nil {
		return
	}
	_ = c.sendFrame(&proto.Frame{Kind: proto.KindEND, StreamID: streamID})
}

// authorize checks a token against the Pod's own list of paired clients, which
// is the source of truth, not the relay's copy.
func (c *Client) authorize(token string) (scope string, ok bool) {
	if token == "" {
		return "", false
	}
	clients, err := loadClients(c.cfg.DeviceID)
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256([]byte(token))
	want := []byte(hex.EncodeToString(sum[:]))
	for _, cl := range clients {
		if subtle.ConstantTimeCompare([]byte(cl.TokenHash), want) == 1 {
			if cl.Scope == "" {
				return proto.ScopeFull, true
			}
			return cl.Scope, true
		}
	}
	return "", false
}
