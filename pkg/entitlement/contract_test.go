package entitlement

import (
	"testing"
	"time"
)

// This token was signed by the Ghost site's own code (ghost-site, tests/entitlement.test.ts
// holds the same pair). If the site changes the wire format, or this package does, the two
// tests disagree and one of them fails: the contract is pinned on both sides.
const (
	siteFixturePublicKey = "6kpsY-KcUgq-9VB7Ey7F-ZVHdq6-vnuSQh7qaRRG0iw"
	siteFixtureToken     = "ge1.eyJ2IjoxLCJwb2QiOiJwb2QtZml4dHVyZS0wMDAxIiwiYWNjdCI6ImFjY3QtZml4dHVyZSIsInBsYW4iOiJjb25uZWN0IiwiaWF0IjoxNzkwMDAwMDAwLCJleHAiOjE3OTAyNTkyMDB9.UaWZrc3uhIt98VM2i9wEvvsbhSgagyBDSeTJ7FvaTtbpZc00LIs0Hvx8WmmLR5sAKJPfNWbb-zKnmPpTA55nBA"
)

func TestVerifiesAPassSignedByTheSite(t *testing.T) {
	keys, err := ParsePublicKeys(siteFixturePublicKey)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1790100000, 0)
	c, err := VerifyForPod(siteFixtureToken, "pod-fixture-0001", keys, now)
	if err != nil {
		t.Fatalf("the relay must accept a pass the site signed: %v", err)
	}
	if c.Account != "acct-fixture" || c.Plan != PlanConnect || c.ExpiresAt != 1790259200 {
		t.Fatalf("unexpected claims %+v", c)
	}
	if _, err := Verify(siteFixtureToken, keys, time.Unix(1790259201, 0)); err != ErrExpired {
		t.Fatalf("an ended pass must be expired, got %v", err)
	}
}
