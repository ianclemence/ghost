package agent

import (
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/modelhealth"
	"github.com/ianclemence/ghost/pkg/provider"
	"github.com/ianclemence/ghost/pkg/providers"
)

func (al *AgentLoop) recordModelCall(model string, started time.Time, resp *providers.LLMResponse, err error, fallback bool) {
	if al == nil || al.modelHealth == nil {
		return
	}
	outcome := modelhealth.OK
	switch {
	case err != nil && provider.ClassifyError(err) == provider.FailRejected:
		outcome = modelhealth.Refused
	case err != nil:
		outcome = modelhealth.Failed
	case resp == nil || (strings.TrimSpace(resp.Content) == "" && len(resp.ToolCalls) == 0):
		outcome = modelhealth.Empty
	}
	al.modelHealth.Record(modelhealth.Call{
		Model: model, Outcome: outcome, LatencyMs: time.Since(started).Milliseconds(), Fallback: fallback,
	})
}

// ModelHealthLine is the doctor's one-line view of how the models are doing.
func (al *AgentLoop) ModelHealthLine() (status, message string) {
	if al == nil || al.modelHealth == nil {
		return "info", "Model health is not being recorded."
	}
	return al.modelHealth.DoctorLine()
}
