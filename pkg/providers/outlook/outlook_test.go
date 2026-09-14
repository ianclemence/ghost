package outlook

import (
	"context"
	"testing"
)

func TestSearchNotConfigured(t *testing.T) {
	t.Setenv("GHOST_CREDENTIALS_DIR", t.TempDir())
	svc := New(Config{})
	if svc.Configured() {
		t.Fatal("empty token must not be configured")
	}
	if _, r := svc.Search(context.Background(), "x", 5); r.Err == nil {
		t.Fatal("search without token must fail")
	}
}

func TestSendNotConfigured(t *testing.T) {
	t.Setenv("GHOST_CREDENTIALS_DIR", t.TempDir())
	svc := New(Config{})
	if _, r := svc.Send(context.Background(), "a@b.c", "s", "b"); r.Err == nil {
		t.Fatal("send without token must fail")
	}
}
