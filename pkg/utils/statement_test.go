package utils

import "testing"

func TestSameStatementNeedsMostContentWordsNotJustOne(t *testing.T) {
	v := "Their favourite coffee is a flat white with oat milk"
	if !SameStatement(v, "Favourite coffee is a flat white with oat milk") {
		t.Fatal("a reworded copy must match")
	}
	for _, other := range []string{"the coffee machine is broken", "Likes oat milk in porridge", "flat white tomorrow"} {
		if SameStatement(v, other) {
			t.Errorf("%q shares only a word or two and must not match", other)
		}
	}
	if SameStatement("green tea", "green tea") {
		t.Fatal("a value with too few content words is left to the exact-match rules")
	}
}
