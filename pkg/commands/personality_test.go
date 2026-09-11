package commands

import (
	"context"
	"strings"
	"testing"
)

func TestSetPersonalityValidatesAndPropagates(t *testing.T) {
	rt := &Runtime{}
	var propagated string
	rt.OnPersonalityChanged = func(name string) { propagated = name }

	if err := rt.SetPersonality("hacker"); err != nil {
		t.Fatalf("builtin must be accepted: %v", err)
	}
	if propagated != "hacker" {
		t.Fatalf("hook must fire with the name, got %q", propagated)
	}
	if err := rt.SetPersonality("HACKER"); err != nil {
		t.Fatalf("names must normalize case: %v", err)
	}
	if err := rt.SetPersonality("nope"); err == nil {
		t.Fatal("unknown personality must be rejected, not stored")
	}
	if rt.Personality == "nope" {
		t.Fatal("rejected selection must not persist")
	}
}

func TestPersonalityHandlerListsAdaptive(t *testing.T) {
	var out string
	rt := &Runtime{OnPersonalityChanged: func(string) {}}
	req := Request{Text: "/personality", Reply: func(s string) error { out = s; return nil }}
	if err := personalityHandler(context.Background(), req, rt); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"default", "adaptive"} {
		if !strings.Contains(out, "`"+n+"`") {
			t.Fatalf("list must include %q: %q", n, out)
		}
	}
	out = ""
	req.Text = "/personality bogus"
	if err := personalityHandler(context.Background(), req, rt); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "unknown personality") {
		t.Fatalf("bogus selection must fail visibly: %q", out)
	}
}
