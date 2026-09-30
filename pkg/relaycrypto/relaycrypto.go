// Package relaycrypto is the sealed envelope for traffic that passes through the
// relay. The relay only forwards bytes; with this, it forwards bytes it cannot
// read or alter.
//
// The Pod has a long-lived X25519 key. Its public half travels to the phone in
// the pairing QR, so the phone pins it and a relay cannot substitute its own.
// For each connection the phone makes a fresh key and both sides derive two
// directional AES-256-GCM keys with HKDF over the shared secret and both public
// keys. Every message carries a strictly increasing counter that is also its
// nonce, so a captured message cannot be replayed, reordered or reflected back.
package relaycrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
)

const (
	info = "ghost-relay-e2e-v1"
	// KeySize is the size of an X25519 public key.
	KeySize = 32
	// Overhead is what sealing adds to a message: the counter and the GCM tag.
	Overhead = 8 + 16
)

var (
	ErrReplay   = errors.New("relaycrypto: message repeated or out of order")
	ErrOpen     = errors.New("relaycrypto: message failed authentication")
	ErrShort    = errors.New("relaycrypto: message too short")
	ErrExhaust  = errors.New("relaycrypto: session used up, start a new one")
	ErrBadKey   = errors.New("relaycrypto: not a valid public key")
	maxMessages = uint64(1) << 40
)

// Identity is a long-lived key pair (the Pod's).
type Identity struct{ priv *ecdh.PrivateKey }

// NewIdentity makes a fresh key pair.
func NewIdentity() (*Identity, error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Identity{priv: k}, nil
}

// LoadIdentity restores a key pair from its 32-byte private key.
func LoadIdentity(private []byte) (*Identity, error) {
	k, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil {
		return nil, ErrBadKey
	}
	return &Identity{priv: k}, nil
}

// Private returns the private key bytes, for sealed storage on the Pod.
func (i *Identity) Private() []byte { return i.priv.Bytes() }

// Public returns the public key bytes, which go in the pairing QR.
func (i *Identity) Public() []byte { return i.priv.PublicKey().Bytes() }

// Session seals outgoing messages and opens incoming ones for one connection.
type Session struct {
	send, recv cipher.AEAD
	mu         sync.Mutex
	sendN      uint64
	recvN      uint64 // the next counter accepted is strictly above this
	gotAny     bool
}

func derive(shared, clientPub, serverPub []byte) (c2s, s2c cipher.AEAD, err error) {
	salt := append(append([]byte{}, clientPub...), serverPub...)
	okm, err := hkdf.Key(sha256.New, shared, salt, info, 64)
	if err != nil {
		return nil, nil, err
	}
	mk := func(b []byte) (cipher.AEAD, error) {
		blk, err := aes.NewCipher(b)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(blk)
	}
	if c2s, err = mk(okm[:32]); err != nil {
		return nil, nil, err
	}
	s2c, err = mk(okm[32:])
	return c2s, s2c, err
}

// Dial starts a session from the phone, given the Pod's pinned public key. It
// returns the session and the ephemeral public key to send to the Pod.
func Dial(serverPublic []byte) (*Session, []byte, error) {
	srv, err := ecdh.X25519().NewPublicKey(serverPublic)
	if err != nil {
		return nil, nil, ErrBadKey
	}
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	shared, err := eph.ECDH(srv)
	if err != nil {
		return nil, nil, ErrBadKey
	}
	c2s, s2c, err := derive(shared, eph.PublicKey().Bytes(), srv.Bytes())
	if err != nil {
		return nil, nil, err
	}
	return &Session{send: c2s, recv: s2c}, eph.PublicKey().Bytes(), nil
}

// Accept starts the Pod's side from the phone's ephemeral public key.
func (i *Identity) Accept(clientPublic []byte) (*Session, error) {
	cli, err := ecdh.X25519().NewPublicKey(clientPublic)
	if err != nil {
		return nil, ErrBadKey
	}
	shared, err := i.priv.ECDH(cli)
	if err != nil {
		return nil, ErrBadKey
	}
	c2s, s2c, err := derive(shared, cli.Bytes(), i.priv.PublicKey().Bytes())
	if err != nil {
		return nil, err
	}
	return &Session{send: s2c, recv: c2s}, nil
}

func nonce(n uint64) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint64(b[4:], n)
	return b
}

// Seal encrypts one message. The counter is sent in the clear as associated
// data, and is the nonce, so it cannot be changed without failing to open.
func (s *Session) Seal(plain []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sendN >= maxMessages {
		return nil, ErrExhaust
	}
	s.sendN++
	hdr := make([]byte, 8)
	binary.BigEndian.PutUint64(hdr, s.sendN)
	return s.send.Seal(hdr, nonce(s.sendN), plain, hdr), nil
}

// Open decrypts one message, rejecting anything altered, repeated or out of
// order.
func (s *Session) Open(sealed []byte) ([]byte, error) {
	if len(sealed) < Overhead {
		return nil, ErrShort
	}
	n := binary.BigEndian.Uint64(sealed[:8])
	s.mu.Lock()
	defer s.mu.Unlock()
	if n == 0 || (s.gotAny && n <= s.recvN) {
		return nil, ErrReplay
	}
	plain, err := s.recv.Open(nil, nonce(n), sealed[8:], sealed[:8])
	if err != nil {
		return nil, ErrOpen
	}
	s.recvN, s.gotAny = n, true
	return plain, nil
}
