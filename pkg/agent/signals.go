package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/cards"
	"github.com/ianclemence/ghost/pkg/commands"
	"github.com/ianclemence/ghost/pkg/constants"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/goals"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/proactive"
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

// SetScheduler wires the authoritative scheduler used by /loop and /remind.
func (al *AgentLoop) SetScheduler(s commands.ScheduleCreator) {
	al.scheduler = s
}

// ScanNotices proposes candidate proactive notices from wired signals.
// Pure scan: no gating, no delivery. Callers gate via MaybeNotify.
func (al *AgentLoop) ScanNotices() []Notice {
	var out []Notice
	out = append(out, al.scanRoutineNotices()...)
	out = append(out, al.scanGoalNotices(time.Now())...)
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
// Quiet hours (PROACTIVE_PREFERENCES.md, user timezone) hold non-urgent
// notices; urgent ones always break through.
func (al *AgentLoop) deliverNotice(nt Notice) {
	if al.bus == nil || al.state == nil {
		return
	}
	if !nt.Urgency && al.ProactiveQuiet(time.Now()) {
		logger.InfoCF("agent", "proactive held: quiet hours",
			map[string]interface{}{"topic": nt.Topic})
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
	// Companion suggestion card: same text, structured for rich clients.
	// Text fallback rides in Content, so plain surfaces read fine.
	if card, err := cards.New(cards.KindSuggestion, "Suggestion", nt.Message); err == nil {
		card.Topic = nt.Topic
		cards.Publish(al.bus, nil, channel, chatID, "", card)
	}
}

// ProactiveQuiet reports whether now falls in user quiet hours.
func (al *AgentLoop) ProactiveQuiet(now time.Time) bool {
	pol := proactive.Load(al.workspace)
	loc := proactive.UserLocation(al.pcStore)
	return proactive.InQuietHours(now, loc, pol)
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
				Message:    fmt.Sprintf("Your routine '%s' is waiting for your approval to proceed. Say the word and I'll release it — or ask me why it's being held.", displayName(r.Name, r.ID)),
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
					Message:    fmt.Sprintf("Your routine '%s' asked for approval on its last run and is still waiting. Say the word and I'll release it — or ask me why it's being held.", displayName(r.Name, r.ID)),
				})
			} else if n := routineRecentErrors(al.schedSvc, r.ID); n > 0 {
				out = append(out, al.failedRoutineNotice(r))
			}
		}
	}
	return out
}

// scanGoalNotices proposes notices for standing goals that need the
// owner: linked routines waiting/failed, or goals with no recorded
// progress in over 48h (stale stewardship). Quiet, expired, or completed
// goals never notify.
func (al *AgentLoop) scanGoalNotices(now time.Time) []Notice {
	if al.workspace == "" {
		return nil
	}
	store := goals.NewStore(al.workspace)
	list, err := store.List(now)
	if err != nil {
		return nil
	}
	var out []Notice
	for _, g := range list {
		if !g.Usable(now) {
			continue
		}
		// Linked routine trouble surfaces under the goal's name.
		troubled := ""
		if al.routineSvc != nil {
			for _, rid := range g.RoutineIDs {
				for _, r := range al.routineSvc.List(al.ghostID(), 100) {
					if r == nil || r.ID != rid {
						continue
					}
					if r.Status == routines.StatusWaiting || r.Status == routines.StatusFailed {
						troubled = displayName(r.Name, r.ID)
					}
				}
			}
		}
		if troubled != "" {
			out = append(out, Notice{
				Topic:      "goal:" + g.ID,
				Priority:   8,
				Confidence: 0.85,
				DedupeKey:  "goal:" + g.ID + ":routine-trouble",
				Message:    fmt.Sprintf("Your goal '%s' needs you: linked routine '%s' requires attention. Want me to dig in?", g.Text, troubled),
			})
			continue
		}
		if len(g.Progress) == 0 {
			continue // new goal, nothing overdue yet
		}
		last := g.Progress[len(g.Progress)-1].At
		if now.Sub(last) > 48*time.Hour {
			out = append(out, Notice{
				Topic:      "goal:" + g.ID,
				Priority:   7,
				Confidence: 0.7,
				DedupeKey:  fmt.Sprintf("goal:%s:stale:%s", g.ID, last.Format("2006-01-02")),
				Message:    fmt.Sprintf("Your goal '%s' hasn't had progress in a while. Still want me on it, or should I pause it?", g.Text),
			})
		}
	}
	return out
}

// WriteEveningReflection appends a structured daily entry (active goals
// by name, routine counts) to today's note, refreshes the MEMORY.md
// digest, and marks reflection done. Silent by design: it never pushes.
// Returns true when it wrote.
func (al *AgentLoop) WriteEveningReflection(now time.Time) bool {
	if al.workspace == "" {
		return false
	}
	pol := proactive.Load(al.workspace)
	loc := proactive.UserLocation(al.pcStore)
	if !proactive.ReflectionDue(al.workspace, now, loc, pol) {
		return false
	}
	var goalNames []string
	if store := goals.NewStore(al.workspace); store != nil {
		if list, err := store.List(now); err == nil {
			for _, g := range list {
				if g.Usable(now) {
					goalNames = append(goalNames, g.Text)
				}
			}
		}
	}
	routinesTotal := 0
	if al.routineSvc != nil {
		routinesTotal = len(al.routineSvc.List(al.ghostID(), 500))
	}
	entry := fmt.Sprintf("## %s — Evening reflection\nActive goals: %d. Routines: %d.\n",
		now.In(loc).Format("15:04"), len(goalNames), routinesTotal)
	for _, name := range goalNames {
		if len(entry) > 1500 {
			break
		}
		entry += "- Goal: " + name + "\n"
	}
	ms := NewMemoryStore(al.workspace)
	if err := ms.AppendToday(entry); err != nil {
		logger.InfoCF("agent", "evening reflection append failed",
			map[string]interface{}{"error": err.Error()})
		return false
	}
	_ = ms.DigestLongTerm()
	proactive.MarkReflected(al.workspace, now, loc)
	return true
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
