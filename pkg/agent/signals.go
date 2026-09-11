package agent

import (
	"fmt"
	"strings"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/constants"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
)

// Proactive signals: the sense organs for intent prediction. Each signal
// proposes a candidate Notice carrying its reason; the noticer gate
// (threshold, confidence, budget, cooldown, dedupe) decides. Signals never
// message the user directly — prediction that misfires is spam from
// someone who lives in your house.
//
// Current signals (wired in reliability order):
//  1. Routine failures and approval waits — the most reliable predictor
//     of "the user will ask about this": their automation needs them.
// More signals (journal sentiment trends, calendar-adjacent intents,
// aspirational goals) attach to ScanNotices as they earn confidence.

// minAffinityForProactive is the relationship floor for non-urgent
// outreach. Below it Ghost stays quiet unless something is urgent:
// a distant relationship that pings you is a stranger, not a friend.
const minAffinityForProactive = 0.35

// SetRoutineSignals wires the routine/scheduler handles the signal scan
// reads. Called once at startup; nil-safe when automation is disabled.
func (al *AgentLoop) SetRoutineSignals(rs *routines.Service, ss *scheduled.Service) {
	al.routineSvc = rs
	al.schedSvc = ss
}

// ScanNotices proposes candidate proactive notices from wired signals.
// Pure scan: no gating, no delivery. Callers gate via MaybeNotify.
func (al *AgentLoop) ScanNotices() []Notice {
	var out []Notice
	out = append(out, al.scanRoutineNotices()...)
	return out
}

// PollProactive scans signals, gates each through the noticer (and the
// affinity floor), and delivers approvals. It returns how many notices
// went out. Safe to call on every heartbeat tick: gating makes repeats
// no-ops.
func (al *AgentLoop) PollProactive() int {
	delivered := 0
	affinity := al.Affect().Affinity
	for _, nt := range al.ScanNotices() {
		if affinity < minAffinityForProactive && !nt.Urgency {
			logger.InfoCF("agent", "proactive skipped: affinity floor",
				map[string]interface{}{"topic": nt.Topic, "affinity": affinity})
			continue
		}
		if al.MaybeNotify(nt) == DecisionNotify {
			delivered++
		}
	}
	return delivered
}

// deliverNotice sends an approved notice to the last active channel.
// Internal channels (cli:direct and kin) never receive proactive pings.
func (al *AgentLoop) deliverNotice(nt Notice) {
	if al.bus == nil || al.state == nil {
		return
	}
	channel, chatID := al.state.GetLastActiveSession()
	if channel == "" || chatID == "" || constants.IsInternalChannel(channel) {
		return
	}
	al.bus.PublishOutbound(bus.OutboundMessage{
		Channel: channel,
		ChatID:  chatID,
		Content: nt.Message,
	})
}

func (al *AgentLoop) ghostID() string {
	if id, err := ghoststate.LoadIdentity(al.workspace); err == nil && id != nil {
		return id.GhostID
	}
	return "ghost-local"
}

// scanRoutineNotices proposes notices for routines needing attention:
// approval waits and failed runs. Waiting is a product overlay recorded
// only in execution history (the item returns to scheduled), so both the
// routine status and recent history are consulted. Every message carries
// its reason so the user can correct the model — the correction is
// itself a learning input.
func (al *AgentLoop) scanRoutineNotices() []Notice {
	if al.routineSvc == nil {
		return nil
	}
	list := al.routineSvc.List(al.ghostID(), 100)
	var out []Notice
	for _, r := range list {
		if r == nil {
			continue
		}
		switch r.Status {
		case routines.StatusWaiting:
			out = append(out, Notice{
				Topic:      "routine:" + r.ID,
				Priority:   7,
				Confidence: 0.9,
				DedupeKey:  "routine:" + r.ID + ":waiting",
				Message: fmt.Sprintf("Your routine '%s' is waiting for your approval to proceed. Say the word and I'll release it — or ask me why it's being held.", displayName(r.Name, r.ID)),
			})
		case routines.StatusFailed:
			out = append(out, al.failedRoutineNotice(r))
		default:
			// Active routines can still be stuck: a recent waiting
			// outcome means an approval is pending; recent errors mean
			// the next trigger will likely fail the same way.
			if al.routineRecentWaiting(r.ID) {
				out = append(out, Notice{
					Topic:      "routine:" + r.ID,
					Priority:   7,
					Confidence: 0.85,
					DedupeKey:  "routine:" + r.ID + ":waiting",
					Message: fmt.Sprintf("Your routine '%s' asked for approval on its last run and is still waiting. Say the word and I'll release it — or ask me why it's being held.", displayName(r.Name, r.ID)),
				})
			} else if n := routineRecentErrors(al.schedSvc, r.ID); n > 0 {
				out = append(out, al.failedRoutineNotice(r))
			}
		}
	}
	return out
}

// routineRecentWaiting reports whether the latest history entry is a wait.
func (al *AgentLoop) routineRecentWaiting(id string) bool {
	if al.schedSvc == nil {
		return false
	}
	hist, err := al.schedSvc.GetHistory(id, 1)
	if err != nil || len(hist) == 0 || hist[0] == nil {
		return false
	}
	return hist[0].Status == "waiting"
}

func (al *AgentLoop) failedRoutineNotice(r *routines.Routine) Notice {
	name := displayName(r.Name, r.ID)
	when := ""
	if r.LastRun != nil {
		when = " on its " + r.LastRun.Format("Mon 15:04") + " run"
	}
	errDetail := strings.TrimSpace(routineLastError(al.schedSvc, r.ID))
	recentFails := routineRecentErrors(al.schedSvc, r.ID)
	msg := fmt.Sprintf("Your routine '%s' failed%s.", name, when)
	if recentFails > 1 {
		msg += fmt.Sprintf(" That's %d recent failures — something structural may be wrong.", recentFails)
	}
	if errDetail != "" {
		msg += " Last error: " + truncateReason(errDetail, 160)
	}
	msg += " Want me to retry it, or dig into the cause?"
	return Notice{
		Topic:      "routine:" + r.ID,
		Priority:   8,
		Confidence: 0.8,
		DedupeKey:  fmt.Sprintf("routine:%s:failed:%d", r.ID, recentFails),
		Message:    msg,
	}
}

func routineLastError(ss *scheduled.Service, id string) string {
	if ss == nil {
		return ""
	}
	item, err := ss.GetItem(id)
	if err != nil || item == nil {
		return ""
	}
	return item.LastError
}

func routineRecentErrors(ss *scheduled.Service, id string) int {
	if ss == nil {
		return 0
	}
	hist, err := ss.GetHistory(id, 5)
	if err != nil {
		return 0
	}
	n := 0
	for _, h := range hist {
		if h != nil && (h.Status == "error" || h.Status == "missed") {
			n++
		}
	}
	return n
}

func displayName(name, id string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	return id
}

func truncateReason(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
