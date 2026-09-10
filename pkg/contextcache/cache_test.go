package contextcache

import (
	"testing"
	"time"
)

func TestGetPutStats(t *testing.T) {
	c := New(time.Minute, 4)
	if _, ok := c.Get("missing"); ok {
		t.Fatal("expected miss")
	}
	c.Put("k", "v")
	if v, ok := c.Get("k"); !ok || v != "v" {
		t.Fatalf("expected hit, got %q %v", v, ok)
	}
	s := c.Stats()
	if s.Hits != 1 || s.Misses != 1 || s.Size != 1 {
		t.Fatalf("unexpected stats: %+v", s)
	}
}

func TestTTLExpiry(t *testing.T) {
	c := New(time.Millisecond, 4)
	c.Put("k", "v")
	time.Sleep(5 * time.Millisecond)
	if _, ok := c.Get("k"); ok {
		t.Fatal("expired entry must miss")
	}
}

func TestLRUEviction(t *testing.T) {
	c := New(0, 2) // no TTL, capacity 2
	c.Put("a", "1")
	c.Put("b", "2")
	c.Get("a") // touch a so b is least-recently-used
	c.Put("c", "3")
	if _, ok := c.Get("b"); ok {
		t.Fatal("least-recently-used entry must be evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Fatal("recently-used entry must survive")
	}
	if _, ok := c.Get("c"); !ok {
		t.Fatal("newest entry must be present")
	}
}

func TestInvalidatePrefix(t *testing.T) {
	c := New(time.Minute, 8)
	c.Put("system:s1", "a")
	c.Put("system:s2", "b")
	c.Put("other:s1", "c")
	if n := c.Invalidate("system:"); n != 2 {
		t.Fatalf("expected 2 invalidated, got %d", n)
	}
	if _, ok := c.Get("system:s1"); ok {
		t.Fatal("system entries must be gone")
	}
	if _, ok := c.Get("other:s1"); !ok {
		t.Fatal("other prefix must remain")
	}
	if c.Stats().Invalidations != 2 {
		t.Fatalf("invalidations not counted: %+v", c.Stats())
	}
}

func TestKeyStableAndBounded(t *testing.T) {
	a := Key("x", "y")
	b := Key("x", "y")
	if a != b {
		t.Fatal("same parts must produce same key")
	}
	if a == Key("x", "z") {
		t.Fatal("different parts must differ")
	}
	if len(a) > 64 {
		t.Fatalf("key must be bounded, got %d chars", len(a))
	}
}
