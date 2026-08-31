package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/doctor"
)

func TestDoctorHandler(t *testing.T) {
	rt := &Runtime{
		Doctor: doctor.New(nil, nil, nil, ""),
	}

	var out string
	req := Request{
		Text:       "/doctor",
		Channel:    "cli",
		ChatID:     "direct",
		SessionKey: "test",
		Reply: func(s string) error {
			out = s
			return nil
		},
	}

	if err := doctorHandler(context.Background(), req, rt); err != nil {
		t.Fatalf("doctorHandler returned error: %v", err)
	}
	if !strings.Contains(out, "Ghost Doctor") {
		t.Fatalf("expected doctor output header, got: %s", out)
	}
	if !strings.Contains(out, "Memory") {
		t.Fatalf("expected memory check in output, got: %s", out)
	}
}
