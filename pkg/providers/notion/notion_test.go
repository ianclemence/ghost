package notion

import (
	"context"
	"testing"
)

func TestValidateNotConfigured(t *testing.T) {
	svc := New(Config{})
	if svc.Configured() {
		t.Fatal("empty token must not be configured")
	}
	if r := svc.Validate(context.Background()); r.Err == nil {
		t.Fatal("validate without token must fail")
	}
}

func TestSearchNotConfigured(t *testing.T) {
	svc := New(Config{})
	if _, r := svc.Search(context.Background(), "x"); r.Err == nil {
		t.Fatal("search without token must fail")
	}
}
