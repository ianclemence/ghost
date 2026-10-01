package relaycrypto

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

// A sealed exchange is one request and one streamed response.
//
// Request (the body the phone posts to the relay):
//
//	[1 byte version = 1][32 bytes the phone's ephemeral public key][sealed message]
//
// Response (what the Pod streams back), a series of frames, each
//
//	[4 bytes big-endian length][sealed message]
//
// and each sealed message opens to one kind byte followed by its content:
//
//	MsgHead  the status and headers
//	MsgData  a chunk of the body
//	MsgEnd   the body is complete
//	MsgErr   the exchange failed, with a short reason
//
// A response without a MsgEnd was cut short and must be treated as an error,
// which is how a relay that drops the end of a stream is noticed.
const (
	EnvelopeVersion = 1

	MsgHead byte = 0
	MsgData byte = 1
	MsgEnd  byte = 2
	MsgErr  byte = 3

	// MaxRequest bounds the sealed request the Pod will open. It is large
	// enough for an owner's photos and files, which travel inside the request
	// (the app's own limit is 60 MB, which is a little more as base64).
	MaxRequest = 96<<20 + 4096
)

var ErrEnvelope = errors.New("relaycrypto: not a sealed request")

// SealRequest is the phone's side: it starts a session against the Pod's
// pinned key and seals the request. The returned session opens the response.
func SealRequest(podPublic, plain []byte) (*Session, []byte, error) {
	sess, eph, err := Dial(podPublic)
	if err != nil {
		return nil, nil, err
	}
	sealed, err := sess.Seal(plain)
	if err != nil {
		return nil, nil, err
	}
	env := make([]byte, 0, 1+KeySize+len(sealed))
	env = append(env, EnvelopeVersion)
	env = append(env, eph...)
	env = append(env, sealed...)
	return sess, env, nil
}

// OpenRequest is the Pod's side. It returns the session that seals the
// response, the request, and the phone's ephemeral key, which the caller feeds
// to a ReplayGuard.
func (i *Identity) OpenRequest(env []byte) (*Session, []byte, []byte, error) {
	if len(env) < 1+KeySize+Overhead || env[0] != EnvelopeVersion {
		return nil, nil, nil, ErrEnvelope
	}
	eph := env[1 : 1+KeySize]
	sess, err := i.Accept(eph)
	if err != nil {
		return nil, nil, nil, err
	}
	plain, err := sess.Open(env[1+KeySize:])
	if err != nil {
		return nil, nil, nil, err
	}
	return sess, plain, append([]byte(nil), eph...), nil
}

// SealMessage seals one response message: its kind byte then its content.
func (s *Session) SealMessage(kind byte, content []byte) ([]byte, error) {
	m := make([]byte, 0, 1+len(content))
	m = append(m, kind)
	m = append(m, content...)
	return s.Seal(m)
}

// OpenMessage opens one response message and splits off its kind.
func (s *Session) OpenMessage(sealed []byte) (byte, []byte, error) {
	m, err := s.Open(sealed)
	if err != nil {
		return 0, nil, err
	}
	if len(m) == 0 {
		return 0, nil, ErrShort
	}
	return m[0], m[1:], nil
}

// Frame prefixes a sealed message with its length for the response stream.
func Frame(sealed []byte) []byte {
	out := make([]byte, 4+len(sealed))
	binary.BigEndian.PutUint32(out, uint32(len(sealed)))
	copy(out[4:], sealed)
	return out
}

// SplitFrames pulls whole frames off the front of buf and returns the rest,
// for a reader that receives the response in arbitrary pieces.
func SplitFrames(buf []byte) (frames [][]byte, rest []byte) {
	for len(buf) >= 4 {
		n := int(binary.BigEndian.Uint32(buf))
		if len(buf) < 4+n {
			break
		}
		frames = append(frames, buf[4:4+n])
		buf = buf[4+n:]
	}
	return frames, buf
}

// ReplayGuard stops a captured request from being sent again. Counters stop
// replay within a session; this stops a relay from opening a new session with
// an old request. Each ephemeral key is accepted once within the window, and
// the caller rejects requests stamped outside the same window.
type ReplayGuard struct {
	mu     sync.Mutex
	window time.Duration
	seen   map[string]time.Time
}

// NewReplayGuard keeps keys for window. Requests must be stamped within
// window of now to be considered at all, so remembering for twice the window
// leaves no gap.
func NewReplayGuard(window time.Duration) *ReplayGuard {
	return &ReplayGuard{window: window, seen: make(map[string]time.Time)}
}

// Window is how far a request's stamp may be from now.
func (g *ReplayGuard) Window() time.Duration { return g.window }

// Fresh reports whether key has not been seen, and remembers it.
func (g *ReplayGuard) Fresh(key []byte, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for k, t := range g.seen {
		if now.Sub(t) > 2*g.window {
			delete(g.seen, k)
		}
	}
	k := string(key)
	if _, dup := g.seen[k]; dup {
		return false
	}
	g.seen[k] = now
	return true
}
