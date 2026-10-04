package agent

import (
	"strings"

	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/personalcontext"
)

// wireMemoryAudit makes every forgetting visible. Until this existed the
// canonical stream carried memory.created and memory.updated but never
// memory.deleted — so a belief Ghost removed simply vanished from the
// record, and the owner's activity feed could not show that it happened.
//
// The hook is global (personalcontext is the storage layer and knows
// nothing about this runtime), so it must be set exactly once per process,
// which is what NewAgentLoop is.
func (al *AgentLoop) wireMemoryAudit() {
	personalcontext.OnForgetReceipt = func(r personalcontext.ForgetReport) {
		if al.governance == nil || al.governance.Events == nil {
			return
		}
		al.governance.Events.Publish(&cevents.Event{
			Type:    cevents.MemoryDeleted,
			GhostID: al.governance.GhostID,
			AgentID: al.governance.AgentID,
			Payload: map[string]interface{}{
				"claim_id": r.ClaimID,
				"title":    forgottenLabel(r),
				"summary":  forgottenSummary(r),
				"reason":   r.Reason,
			},
		})
	}
}

// forgottenLabel is the short handle for a removed belief: its predicate,
// or the subject when the predicate is empty. Never the value — the value
// is the thing being removed.
func forgottenLabel(r personalcontext.ForgetReport) string {
	if p := strings.TrimSpace(r.Predicate); p != "" {
		return p
	}
	return strings.TrimSpace(r.Subject)
}

// forgottenSummary is the one-line record the activity feed shows: what was
// done to make the forgetting hold — the tombstone that stops old evidence
// resurrecting it, and the derived stores that were rebuilt so it stops
// being served. It says what was done, not what was said.
func forgottenSummary(r personalcontext.ForgetReport) string {
	var parts []string
	if r.Tombstoned {
		parts = append(parts, "Recorded so old messages cannot bring it back")
	}
	if len(r.DerivativesRebuilt) > 0 {
		parts = append(parts, "Rebuilt "+strings.Join(r.DerivativesRebuilt, ", "))
	}
	if len(parts) == 0 {
		return "Removed from what Ghost remembers."
	}
	return strings.Join(parts, ". ") + "."
}
