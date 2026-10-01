// Package entitlement is the signed pass that lets a Pod use the hosted relay.
//
// The Ghost site signs a token when a subscription is paid for; the relay
// checks the signature and nothing else about the owner. The token names a
// Pod, carries an opaque account id and an expiry, and is short-lived so a
// lapsed subscription simply stops being renewed. The relay holds only the
// public key. See docs/CONNECT.md for the flow.
//
// Wire format:
//
//	ge1.<base64url(payload JSON)>.<base64url(Ed25519 signature)>
//
// The signature covers the string "ge1." + the payload segment exactly as
// sent, so no canonical JSON form is needed.
package entitlement

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Prefix identifies the token version.
const Prefix = "ge1"

// PlanConnect is the hosted relay plan.
const PlanConnect = "connect"

// MaxClockSkew is how far in the future an issue time may be.
const MaxClockSkew = 5 * time.Minute

var (
	ErrMalformed = errors.New("entitlement: malformed token")
	ErrSignature = errors.New("entitlement: signature does not match a trusted key")
	ErrExpired   = errors.New("entitlement: expired")
	ErrNotYet    = errors.New("entitlement: issued in the future")
	ErrPod       = errors.New("entitlement: issued for a different Pod")
	ErrNoKeys    = errors.New("entitlement: no trusted keys configured")
)

// Claims is the signed payload.
type Claims struct {
	V         int    `json:"v"`
	KeyID     string `json:"kid,omitempty"`
	Pod       string `json:"pod"`  // the device id the relay knows the Pod by
	Account   string `json:"acct"` // opaque; the relay never learns who this is
	Plan      string `json:"plan"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

// Expiry is ExpiresAt as a time.
func (c *Claims) Expiry() time.Time { return time.Unix(c.ExpiresAt, 0) }

var b64 = base64.RawURLEncoding

// Sign produces a token. The site does this; it lives here so the contract has
// one definition and the relay's tests can mint tokens.
func Sign(priv ed25519.PrivateKey, c Claims) (string, error) {
	if c.Pod == "" || c.Account == "" {
		return "", fmt.Errorf("%w: pod and acct are required", ErrMalformed)
	}
	if c.V == 0 {
		c.V = 1
	}
	if c.Plan == "" {
		c.Plan = PlanConnect
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	head := Prefix + "." + b64.EncodeToString(payload)
	sig := ed25519.Sign(priv, []byte(head))
	return head + "." + b64.EncodeToString(sig), nil
}

// Verify checks a token against the trusted keys and the clock. When the
// claims name a key id, only keys are tried in order regardless: key ids are a
// hint for humans, not a way to choose which key to trust.
func Verify(token string, keys []ed25519.PublicKey, now time.Time) (*Claims, error) {
	if len(keys) == 0 {
		return nil, ErrNoKeys
	}
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 || parts[0] != Prefix {
		return nil, ErrMalformed
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, ErrMalformed
	}
	head := []byte(parts[0] + "." + parts[1])
	ok := false
	for _, k := range keys {
		if len(k) == ed25519.PublicKeySize && ed25519.Verify(k, head, sig) {
			ok = true
			break
		}
	}
	if !ok {
		return nil, ErrSignature
	}
	raw, err := b64.DecodeString(parts[1])
	if err != nil {
		return nil, ErrMalformed
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil || c.V != 1 || c.Pod == "" || c.Account == "" {
		return nil, ErrMalformed
	}
	if now.After(c.Expiry()) {
		return nil, ErrExpired
	}
	if time.Unix(c.IssuedAt, 0).After(now.Add(MaxClockSkew)) {
		return nil, ErrNotYet
	}
	return &c, nil
}

// VerifyForPod is Verify plus a check that the token belongs to this Pod.
func VerifyForPod(token, pod string, keys []ed25519.PublicKey, now time.Time) (*Claims, error) {
	c, err := Verify(token, keys, now)
	if err != nil {
		return nil, err
	}
	if c.Pod != pod {
		return nil, ErrPod
	}
	return c, nil
}

// ParsePublicKeys reads one or more Ed25519 public keys written as base64url,
// standard base64 or hex, separated by commas, spaces or newlines. Several keys
// let the signing key be rotated without a flag day.
func ParsePublicKeys(s string) ([]ed25519.PublicKey, error) {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == '\r' })
	var out []ed25519.PublicKey
	for _, f := range fields {
		k, err := decodeKey(f)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
}

func decodeKey(s string) (ed25519.PublicKey, error) {
	var raw []byte
	if len(s) == 64 {
		if b, err := hex.DecodeString(s); err == nil {
			raw = b
		}
	}
	if raw == nil {
		for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding} {
			if b, err := enc.DecodeString(s); err == nil {
				raw = b
				break
			}
		}
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("entitlement: %q is not an Ed25519 public key", truncate(s))
	}
	return ed25519.PublicKey(raw), nil
}

// EncodePublicKey writes a public key the way the site and the relay config
// expect it.
func EncodePublicKey(k ed25519.PublicKey) string { return b64.EncodeToString(k) }

func truncate(s string) string {
	if len(s) > 12 {
		return s[:12] + "…"
	}
	return s
}
