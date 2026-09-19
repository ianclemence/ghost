package connector

import "testing"

func TestSignAndVerify(t *testing.T) {
	pub, priv, err := GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	m := validNative()
	if err := Sign(m, priv, "alice"); err != nil {
		t.Fatal(err)
	}
	if m.Provenance.Hash == "" || m.Provenance.Signature == "" || m.Provenance.SignedBy != "alice" {
		t.Fatalf("signature not recorded: %+v", m.Provenance)
	}
	if err := VerifySignature(m, pub); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	// Any tampering invalidates the signature.
	m.DisplayName = "Tampered"
	if err := VerifySignature(m, pub); err == nil {
		t.Fatal("tampered manifest must fail verification")
	}
}

func TestVerifyWrongKey(t *testing.T) {
	_, priv, _ := GenerateSigningKey()
	otherPub, _, _ := GenerateSigningKey()
	m := validNative()
	if err := Sign(m, priv, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := VerifySignature(m, otherPub); err == nil {
		t.Fatal("a different key must fail verification")
	}
}

func TestHashStableAcrossSign(t *testing.T) {
	_, priv, _ := GenerateSigningKey()
	m := validNative()
	before, err := manifestHash(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := Sign(m, priv, "alice"); err != nil {
		t.Fatal(err)
	}
	after, _ := manifestHash(m)
	if before != after {
		t.Fatalf("content hash changed on sign: %s != %s", before, after)
	}
	if m.Provenance.Hash != before {
		t.Fatalf("recorded hash %s != %s", m.Provenance.Hash, before)
	}
}

func TestSignRequiresName(t *testing.T) {
	_, priv, _ := GenerateSigningKey()
	if err := Sign(validNative(), priv, "  "); err == nil {
		t.Fatal("empty signer must be rejected")
	}
}

func TestDecodeKeys(t *testing.T) {
	pub, priv, _ := GenerateSigningKey()
	if _, err := DecodePrivateKey(EncodePrivateKey(priv)); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePublicKey(EncodePublicKey(pub)); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePrivateKey("zz"); err == nil {
		t.Fatal("bad private key must fail")
	}
	if _, err := DecodePublicKey("00"); err == nil {
		t.Fatal("short public key must fail")
	}
}
