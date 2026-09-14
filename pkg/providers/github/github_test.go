package github

import (
	"context"
	"testing"
)

func TestValidateNotConfigured(t *testing.T) {
	svc := New(Config{})
	if svc.Configured() {
		t.Fatal("empty PAT must not be configured")
	}
	if r := svc.Validate(context.Background()); r.Err == nil {
		t.Fatal("validate without PAT must fail")
	}
}

func TestSearchCodeNotConfigured(t *testing.T) {
	svc := New(Config{})
	if _, r := svc.SearchCode(context.Background(), "x", ""); r.Err == nil {
		t.Fatal("search without PAT must fail")
	}
}
