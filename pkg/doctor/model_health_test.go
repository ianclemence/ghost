package doctor

import (
	"context"
	"testing"
)

func TestModelHealthCheckReportsWhatTheLedgerSays(t *testing.T) {
	d := &Doctor{ModelHealth: func() (string, string) { return "warn", "m: failing 40% of recent calls" }}
	got := d.checkModelHealth(context.Background())
	if got.Status != "warn" || got.Message == "" || got.Name != "model_health" {
		t.Fatalf("doctor hid the model's drift: %+v", got)
	}
}

func TestModelHealthCheckWithoutALedgerIsInformational(t *testing.T) {
	if got := (&Doctor{}).checkModelHealth(context.Background()); got.Status != "info" || got.Message != "" {
		t.Fatalf("an unwired check must not claim health: %+v", got)
	}
}
