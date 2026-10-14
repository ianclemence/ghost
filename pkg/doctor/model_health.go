package doctor

import (
	"context"
	"time"
)

func (d *Doctor) checkModelHealth(_ context.Context) CheckResult {
	start := time.Now()
	status, msg := "info", ""
	if d.ModelHealth != nil {
		status, msg = d.ModelHealth()
	}
	return CheckResult{Name: "model_health", Label: "Model health", Status: status, Message: msg, Latency: time.Since(start).Milliseconds()}
}
