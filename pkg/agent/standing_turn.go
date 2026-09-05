package agent

// Natural-language standing permissions: "Always let Ghost add calendar
// events." Deterministic proposal → durable pending → explicit "yes" →
// runtime re-validates scope → narrow scoped grant persisted. Broad,
// malformed, cross-integration, and forged requests reject or stay
// ordinary chat. The model never authorizes; the registry does.

import (
	"encoding/json"
	"strings"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/skills"
)

// standingBroker opens the loop-local broker handle (same SQLite file
// the gateway uses — one durable truth, restart-safe).
func (al *AgentLoop) standingBroker() (*permissions.Broker, error) {
	var err error
	al.standingBrokerOnce.Do(func() {
		var b *permissions.Broker
		b, err = permissions.Open(al.db.DB, permissions.ModeAsk, 0)
		al.standingBrokerInst = b
	})
	if err != nil {
		return nil, err
	}
	return al.standingBrokerInst, nil
}

const standingPendingCapability = "permission.standing"

// tryStandingTurn handles standing-permission requests deterministically.
func (al *AgentLoop) tryStandingTurn(msg bus.InboundMessage) (string, bool) {
	text := strings.TrimSpace(msg.Content)
	if text == "" {
		return "", false
	}
	session := msg.SessionKey
	store := skills.NewPendingStore(al.workspace)

	if pending, ok := store.OpenForSession(session); ok && pending.Capability == standingPendingCapability {
		lower := strings.ToLower(strings.TrimSpace(text))
		switch lower {
		case "yes", "y", "confirm", "do it", "approve", "ok", "sure":
			return al.confirmStandingGrant(store, pending)
		case "no", "n", "cancel", "never mind", "nevermind":
			store.Cancel(pending.ID)
			return "No problem — nothing changed.", true
		default:
			return "", false
		}
	}
	proposal, rejection, ok := permissions.ProposeStanding(text)
	if !ok {
		return "", false
	}
	if len(proposal.Grants) == 0 {
		return rejection.Reason + " For example: " + strings.Join(rejection.Options, "; ") + ".", true
	}
	raw, _ := json.Marshal(proposal.Grants)
	cont := map[string]string{"grants": string(raw)}
	if proposal.Deny {
		cont["deny"] = "true"
	}
	store.Create(session, standingPendingCapability, "permissions", "",
		proposal.Summary, text, 0, cont)
	question := proposal.Summary + " Say yes to confirm."
	if proposal.Deny {
		question = proposal.Summary + " Say yes to confirm."
	}
	return question, true
}

func (al *AgentLoop) confirmStandingGrant(store *skills.PendingStore, pending *skills.PendingRequest) (string, bool) {
	var grants []permissions.StandingGrant
	if err := json.Unmarshal([]byte(pending.Continuation["grants"]), &grants); err != nil || len(grants) == 0 {
		store.Cancel(pending.ID)
		return "That permission didn't look right, so I didn't store anything.", true
	}
	deny := pending.Continuation["deny"] == "true"
	broker, err := al.standingBroker()
	if err != nil {
		return "", false
	}
	stored := 0
	for _, g := range grants {
		// Runtime re-validation at confirm time: the registry decides,
		// not the pending payload.
		if !skills.HasCapability(g.Capability) {
			continue
		}
		if err := broker.GrantStanding(g.Capability, g.Action, g.Scope, deny); err != nil {
			continue
		}
		stored++
	}
	store.Complete(pending.ID)
	if stored == 0 {
		return "I couldn't store that permission — the scope isn't valid. Nothing changed.", true
	}
	if deny {
		return "Understood. Ghost will never do that without asking first. Nothing else changed.", true
	}
	return "Done. " + pending.Question, true
}
