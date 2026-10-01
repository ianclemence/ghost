package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/ianclemence/ghost/pkg/relay/proto"
	"github.com/ianclemence/ghost/pkg/relaycrypto"
)

// entitlementCheckEvery is how often a metered tunnel is checked against its
// expiry. A variable so tests can shorten it.
var entitlementCheckEvery = 15 * time.Second

// watchEntitlement closes a tunnel when its entitlement runs out. The Pod
// renews through OpEntitlement well before that, so a tunnel that reaches its
// expiry belongs to a subscription that has ended or a Pod that could not
// reach the site.
func (s *Server) watchEntitlement(t *DeviceTunnel) {
	tick := time.NewTicker(entitlementCheckEvery)
	defer tick.Stop()
	for {
		select {
		case <-t.Done:
			return
		case <-tick.C:
			exp := t.Expires.Load()
			if exp == 0 || s.cfg.now().Unix() <= exp {
				continue
			}
			_ = proto.WriteCTLWS(t.Conn, 0, &proto.Control{Op: proto.OpError, Message: "entitlement expired"})
			log.Printf("relay: device %s entitlement ended, closing tunnel", t.DeviceID)
			t.Close()
			return
		}
	}
}

// handleSealed carries an end-to-end encrypted exchange between a paired app
// and its Pod. The relay authenticates the app's token so it knows where to
// send the bytes and whom to rate-limit, but it cannot see the request, the
// response, or which part of the Pod's API is being used. Scope is enforced by
// the Pod, which checks the token again inside the sealed request.
func (s *Server) handleSealed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
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
	if !s.requestLimit.allow(deviceID+"|"+HashToken(clientToken)[:16], s.cfg.now()) {
		tooMany(w)
		return
	}
	tunnel := s.tunnels.GetTunnel(deviceID)
	if tunnel == nil {
		http.Error(w, `{"error":"device offline"}`, http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, relaycrypto.MaxRequest+1))
	if err != nil || len(body) == 0 || len(body) > relaycrypto.MaxRequest {
		http.Error(w, `{"error":"bad body"}`, http.StatusBadRequest)
		return
	}

	streamID := s.streams.nextStreamID()
	key := fmt.Sprintf("%s:%d", deviceID, streamID)
	st := &streamState{id: streamID, tunnel: tunnel, ch: make(chan *proto.Frame, 64), done: make(chan struct{})}
	s.streams.register(key, st)
	defer s.streams.remove(key)
	defer st.close()

	meta, _ := json.Marshal(&proto.HTTPMetadata{Type: proto.StreamSealed})
	for _, f := range []*proto.Frame{
		{Kind: proto.KindOPEN, StreamID: streamID, Payload: meta},
		{Kind: proto.KindDATA, StreamID: streamID, Payload: body},
		{Kind: proto.KindEND, StreamID: streamID},
	} {
		if err := tunnel.SendFrame(f); err != nil {
			http.Error(w, `{"error":"tunnel write"}`, http.StatusBadGateway)
			return
		}
	}

	// The Pod answers with an OPEN, then sealed frames, then END. Only the OPEN
	// is waited for with a deadline; after that the Pod sets the pace (a model
	// can think for a while between chunks).
	timeout := time.After(30 * time.Second)
	for opened := false; !opened; {
		select {
		case <-timeout:
			http.Error(w, `{"error":"device timeout"}`, http.StatusGatewayTimeout)
			return
		case <-st.done:
			http.Error(w, `{"error":"stream closed"}`, http.StatusBadGateway)
			return
		case f := <-st.ch:
			switch f.Kind {
			case proto.KindOPEN:
				opened = true
			case proto.KindERROR, proto.KindEND:
				http.Error(w, `{"error":"device refused"}`, http.StatusBadGateway)
				return
			}
		}
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Ghost-Relay", "sealed")
	w.WriteHeader(http.StatusOK)
	flusher, canFlush := w.(http.Flusher)
	for {
		select {
		case <-st.done:
			return
		case <-r.Context().Done():
			return
		case f := <-st.ch:
			switch f.Kind {
			case proto.KindDATA:
				w.Write(f.Payload)
				if canFlush {
					flusher.Flush()
				}
			case proto.KindEND, proto.KindERROR:
				return
			}
		}
	}
}
