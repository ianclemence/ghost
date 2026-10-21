package agent

import (
	"testing"
	"time"
)

func TestUnattendedLimit(t *testing.T) {
	t.Setenv("GHOST_UNATTENDED_TIMEOUT", "")
	if UnattendedLimit() != DefaultUnattendedLimit {
		t.Fatal("default")
	}
	t.Setenv("GHOST_UNATTENDED_TIMEOUT", "45m")
	if UnattendedLimit() != 45*time.Minute {
		t.Fatal("override")
	}
	t.Setenv("GHOST_UNATTENDED_TIMEOUT", "nonsense")
	if UnattendedLimit() != DefaultUnattendedLimit {
		t.Fatal("bad value must fall back")
	}
}

func TestUnattendedContextExpires(t *testing.T) {
	t.Setenv("GHOST_UNATTENDED_TIMEOUT", "30ms")
	ctx, cancel := UnattendedContext(t.Context())
	defer cancel()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("deadline not applied")
	}
}
