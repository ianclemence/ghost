package entitlement

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"
)

func keys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func claims(now time.Time, ttl time.Duration) Claims {
	return Claims{Pod: "pod-1", Account: "acct-1", IssuedAt: now.Unix(), ExpiresAt: now.Add(ttl).Unix()}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv := keys(t)
	now := time.Now()
	tok, err := Sign(priv, claims(now, time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok, "ge1.") {
		t.Fatalf("token should start with ge1., got %q", tok)
	}
	c, err := Verify(tok, []ed25519.PublicKey{pub}, now)
	if err != nil {
		t.Fatal(err)
	}
	if c.Pod != "pod-1" || c.Account != "acct-1" || c.Plan != PlanConnect {
		t.Fatalf("unexpected claims %+v", c)
	}
}

func TestVerifyRejects(t *testing.T) {
	pub, priv := keys(t)
	otherPub, _ := keys(t)
	now := time.Now()
	good, _ := Sign(priv, claims(now, time.Hour))
	expired, _ := Sign(priv, claims(now.Add(-2*time.Hour), time.Hour))
	future, _ := Sign(priv, claims(now.Add(time.Hour), time.Hour))

	cases := []struct {
		name string
		tok  string
		keys []ed25519.PublicKey
		want error
	}{
		{"wrong key", good, []ed25519.PublicKey{otherPub}, ErrSignature},
		{"expired", expired, []ed25519.PublicKey{pub}, ErrExpired},
		{"issued in the future", future, []ed25519.PublicKey{pub}, ErrNotYet},
		{"garbage", "nonsense", []ed25519.PublicKey{pub}, ErrMalformed},
		{"wrong version", "ge2." + strings.SplitN(good, ".", 2)[1], []ed25519.PublicKey{pub}, ErrMalformed},
		{"no keys", good, nil, ErrNoKeys},
	}
	for _, tc := range cases {
		if _, err := Verify(tc.tok, tc.keys, now); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	pub, priv := keys(t)
	now := time.Now()
	a, _ := Sign(priv, claims(now, time.Hour))
	c := claims(now, 100*24*time.Hour)
	c.Pod = "someone-elses-pod"
	b, _ := Sign(priv, c) // validly signed, but a different payload
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	forged := pa[0] + "." + pb[1] + "." + pa[2] // payload of b, signature of a
	if _, err := Verify(forged, []ed25519.PublicKey{pub}, now); !errors.Is(err, ErrSignature) {
		t.Fatalf("a swapped payload must fail the signature, got %v", err)
	}
}

func TestVerifyForPod(t *testing.T) {
	pub, priv := keys(t)
	now := time.Now()
	tok, _ := Sign(priv, claims(now, time.Hour))
	if _, err := VerifyForPod(tok, "pod-1", []ed25519.PublicKey{pub}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyForPod(tok, "pod-2", []ed25519.PublicKey{pub}, now); !errors.Is(err, ErrPod) {
		t.Fatalf("a token for another Pod must be refused, got %v", err)
	}
}

func TestKeyRotationAcceptsEitherKey(t *testing.T) {
	oldPub, oldPriv := keys(t)
	newPub, newPriv := keys(t)
	now := time.Now()
	trusted := []ed25519.PublicKey{oldPub, newPub}
	for _, p := range []ed25519.PrivateKey{oldPriv, newPriv} {
		tok, _ := Sign(p, claims(now, time.Hour))
		if _, err := Verify(tok, trusted, now); err != nil {
			t.Fatal(err)
		}
	}
}

func TestParsePublicKeys(t *testing.T) {
	a, _ := keys(t)
	b, _ := keys(t)
	got, err := ParsePublicKeys(EncodePublicKey(a) + ", " + EncodePublicKey(b))
	if err != nil || len(got) != 2 || !got[0].Equal(a) || !got[1].Equal(b) {
		t.Fatalf("round trip failed: %v %v", got, err)
	}
	if _, err := ParsePublicKeys("not-a-key"); err == nil {
		t.Fatal("a bad key must be refused")
	}
}

func TestSignRequiresPodAndAccount(t *testing.T) {
	_, priv := keys(t)
	if _, err := Sign(priv, Claims{Account: "a"}); err == nil {
		t.Fatal("missing pod must be refused")
	}
	if _, err := Sign(priv, Claims{Pod: "p"}); err == nil {
		t.Fatal("missing account must be refused")
	}
}
