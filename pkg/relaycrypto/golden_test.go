package relaycrypto

import (
	"bytes"
	"crypto/ecdh"
	"encoding/hex"
	"testing"
)

// A fixed exchange, byte for byte. The phone app (ghost-app, lib/sealedFetch.test.ts)
// pins the very same hex, so the two implementations cannot drift apart unnoticed.
const (
	goldenPodSeed  = "0101010101010101010101010101010101010101010101010101010101010101"
	goldenEphSeed  = "0202020202020202020202020202020202020202020202020202020202020202"
	goldenPlain    = "hello ghost"
	goldenEnvelope = "01ce8d3ad1ccb633ec7b70c17814a5c76ecd029685050d344745ba05870e587d590000000000000001b0fb8fe7adf9266b3d780e9ec9f2f1c8cceffdf65c280bf38baad1"
	goldenReply    = "00000000000000018613e306209f58e4f8c4fce91eac47740ef7441563"
)

func TestGoldenExchange(t *testing.T) {
	podSeed, _ := hex.DecodeString(goldenPodSeed)
	ephSeed, _ := hex.DecodeString(goldenEphSeed)
	pod, err := LoadIdentity(podSeed)
	if err != nil {
		t.Fatal(err)
	}
	eph, _ := ecdh.X25519().NewPrivateKey(ephSeed)
	sess, ephPub, err := dialWith(pod.Public(), eph)
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := sess.Seal([]byte(goldenPlain))
	env := append(append([]byte{EnvelopeVersion}, ephPub...), sealed...)

	podSess, plain, _, err := pod.OpenRequest(env)
	if err != nil || string(plain) != goldenPlain {
		t.Fatalf("pod could not open: %v", err)
	}
	reply, _ := podSess.SealMessage(MsgData, []byte("pong"))

	t.Logf("podPublic=%s", hex.EncodeToString(pod.Public()))
	t.Logf("envelope=%s", hex.EncodeToString(env))
	t.Logf("reply=%s", hex.EncodeToString(reply))

	{
		want, _ := hex.DecodeString(goldenEnvelope)
		if !bytes.Equal(env, want) {
			t.Fatalf("envelope changed:\n got %x\nwant %x", env, want)
		}
		wantReply, _ := hex.DecodeString(goldenReply)
		if !bytes.Equal(reply, wantReply) {
			t.Fatalf("reply changed:\n got %x\nwant %x", reply, wantReply)
		}
	}
}
