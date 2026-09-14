package spotify

import (
	"context"
	"testing"
)

func TestNowPlayingNotConfigured(t *testing.T) {
	t.Setenv("GHOST_CREDENTIALS_DIR", t.TempDir())
	svc := New(Config{})
	if svc.Configured() {
		t.Fatal("empty token must not be configured")
	}
	if _, r := svc.NowPlaying(context.Background()); r.Err == nil {
		t.Fatal("status without token must fail")
	}
}

func TestControlNotConfigured(t *testing.T) {
	t.Setenv("GHOST_CREDENTIALS_DIR", t.TempDir())
	svc := New(Config{})
	if r := svc.Control(context.Background(), "pause", ""); r.Err == nil {
		t.Fatal("control without token must fail")
	}
}
