package agent

// Natural-language routine creation: "Every Monday at 9 remind me to
// review my finances" becomes a durable routine through the EXISTING
// routine domain (/v1/routines → scheduler → capability execution).
// No second scheduler, no LLM scheduler manipulation: deterministic
// intent parsing, one confirmation, then routines.Service.Create.
//
// Reminders vs routines stay distinct: one-time patterns never reach
// here (routines.ParseIntent rejects them); the existing reminder path
// owns those.

import (
	"strconv"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
	"github.com/ianclemence/ghost/pkg/skills"
)

const routinePendingCapability = "routine.create"

// isStandingGoalText reports explicit standing-goal language. Such turns
// route to the goal tool (durable intent + progress) via the model, never
// to the routine fast-path — even when they contain schedule words like
// "every evening" that would otherwise parse as a reminder.
func isStandingGoalText(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if strings.Contains(lower, "standing goal") {
		return true
	}
	for _, prefix := range []string{"set a goal", "set my goal", "create a goal", "add a goal", "track a goal", "my goal is"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// isStateQuestion reports replies asking about existing state rather than
// supplying new content: trailing "?", leading interrogatives, or
// "what are my ..." recall phrasing. Such replies must not become routine
// task text.
func isStateQuestion(text string) bool {
	trimmed := strings.TrimSpace(text)
	if strings.HasSuffix(trimmed, "?") {
		return true
	}
	lower := strings.ToLower(trimmed)
	for _, q := range []string{"what ", "which ", "when ", "where ", "who ", "how ", "list ", "show me ", "do i have ", "have i "} {
		if strings.HasPrefix(lower, q) {
			return true
		}
	}
	return false
}

// hasSchedulingAsk reports whether the text explicitly asks Ghost to set
// up recurrence. The routine fast-path runs pre-LLM with no semantic
// understanding, so a bare recurring pattern ("every morning I feel
// groggy", "daily standup is painful") must NOT propose: describing a
// schedule is not delegating one. Only explicit ask verbs route here;
// everything else falls through to the model, which answers
// conversationally or uses the governed schedule tool. Same guard family
// as isStandingGoalText: conservative syntactic gates on deterministic
// paths that would otherwise write durable state from vague chat. The
// confirmation question ("Say yes to confirm") remains the backstop, so
// the residual edge (reminiscing that happens to contain an ask verb)
// still needs a yes.
func hasSchedulingAsk(text string) bool {
	lower := strings.ToLower(text)
	for _, ask := range []string{"remind", "notify", "alert", "wake", "nudge",
		"schedul", "recur", "set up", "don't forget", "don't let me forget", "make sure"} {
		if strings.Contains(lower, ask) {
			return true
		}
	}
	return false
}

// tryRoutineTurn handles routine intents deterministically. Returns
// (answer, handled).
func (al *AgentLoop) tryRoutineTurn(msg bus.InboundMessage) (string, bool) {
	text := strings.TrimSpace(msg.Content)
	if text == "" {
		return "", false
	}
	// Standing goals belong to the goal tool, not the routine fast-path:
	// "set a standing goal to X every evening" is a durable intent with
	// progress tracking, not a reminder. Let it fall through to the model.
	if isStandingGoalText(text) {
		return "", false
	}
	session := msg.SessionKey
	store := skills.NewPendingStore(al.workspace)

	// 1. Open proposal for this session?
	if pending, ok := store.OpenForSession(session); ok && pending.Capability == routinePendingCapability {
		lower := strings.ToLower(strings.TrimSpace(text))
		switch lower {
		case "yes", "y", "confirm", "do it", "approve", "ok", "sure":
			return al.confirmRoutineProposal(store, pending, msg)
		case "no", "n", "cancel", "never mind", "nevermind":
			store.Cancel(pending.ID)
			return "No problem — I didn't schedule anything.", true
		}
		// A new message that is itself a complete routine intent supersedes
		// the stale open proposal. Without this, an abandoned proposal
		// hijacked the next routine request and its text was appended to the
		// old intent ("prepare my brief. Please create it now Every Monday").
		// The superseding intent needs the same explicit ask as a fresh one:
		// narration must not seize an open proposal either.
		if intent := routines.ParseIntent(text, time.Now(), routineTimezone(msg)); intent.IsRoutine && !intent.NeedsClarification && intent.Task != "" && hasSchedulingAsk(text) {
			store.Cancel(pending.ID)
			return al.proposeRoutine(store, nil, msg, intent.Task, intent)
		}
		// Awaiting-task proposal: the reply IS the task — unless it
		// is a question about existing state ("what are my goals?"),
		// which must fall through to the model instead of becoming
		// a garbled reminder ("remind you to What are my goals?").
		if pending.MissingField == "task" && len(text) > 2 && !isStateQuestion(text) {
			return al.proposeRoutine(store, pending, msg, text)
		}
		return "", false
	}

	// 2. Fresh intent. A recurring pattern alone is never enough: without
	// an explicit scheduling ask the text is narration, not delegation,
	// and falls through to the model (audit C3), which answers
	// conversationally or uses the governed schedule tool. The
	// "What should happen?" clarify below now only follows a genuine ask
	// with missing details ("remind me every Monday"), never a bare
	// schedule mention ("Every Monday at 9").
	tz := routineTimezone(msg)
	intent := routines.ParseIntent(text, time.Now(), tz)
	if !intent.IsRoutine {
		return "", false
	}
	if !hasSchedulingAsk(text) {
		return "", false
	}
	if intent.NeedsClarification || intent.Task == "" {
		clause := intent.ScheduleText
		if clause == "" {
			clause = "on that schedule"
		}
		store.Create(session, routinePendingCapability, "routines", "task",
			"What should happen "+clause+"?", text, 0,
			map[string]string{"schedule_text": clause, "timezone": tz})
		return "What should happen " + clause + "?", true
	}
	return al.proposeRoutine(store, nil, msg, intent.Task, intent)
}

func (al *AgentLoop) proposeRoutine(store *skills.PendingStore, old *skills.PendingRequest, msg bus.InboundMessage, task string, in ...routines.Intent) (string, bool) {
	tz := routineTimezone(msg)
	var intent routines.Intent
	if len(in) > 0 && in[0].Task != "" {
		intent = in[0]
	} else {
		base := task
		if old != nil {
			base = old.Intent + " " + task
		}
		intent = routines.ParseIntent(base, time.Now(), tz)
		if intent.Task == "" {
			intent.Task = strings.TrimSpace(task)
		}
	}
	if intent.Task == "" {
		return "", false
	}
	if old != nil {
		store.Cancel(old.ID)
	}
	cont := map[string]string{
		"name":          truncateRoutine(intent.Task, 60),
		"instruction":   "remind me to " + intent.Task,
		"timezone":      ifEmpty(intent.Timezone, tz),
		"schedule_text": intent.ScheduleText,
		"kind":          string(intent.Schedule.Kind),
		"expr":          intent.Schedule.Expr,
	}
	if intent.Schedule.Every > 0 {
		cont["every_seconds"] = strconv.FormatInt(int64(intent.Schedule.Every/time.Second), 10)
	}
	if intent.Schedule.At != nil {
		cont["at"] = intent.Schedule.At.Format(time.RFC3339)
	}
	clause := intent.ScheduleText
	if clause == "" {
		clause = "on that schedule"
	}
	question := "I'll remind you to " + intent.Task + " " + clause + ". Say yes to confirm."
	store.Create(msg.SessionKey, routinePendingCapability, "routines", "",
		question, msg.Content, 0, cont)
	return question, true
}

func (al *AgentLoop) confirmRoutineProposal(store *skills.PendingStore, pending *skills.PendingRequest, msg bus.InboundMessage) (string, bool) {
	c := pending.Continuation
	tz := ifEmpty(c["timezone"], routineTimezone(msg))
	var sched scheduled.Schedule
	switch c["kind"] {
	case string(scheduled.ScheduleCron):
		if strings.TrimSpace(c["expr"]) == "" {
			store.Cancel(pending.ID)
			return "I couldn't schedule that — the timing wasn't clear. Try again?", true
		}
		sched = scheduled.Schedule{Kind: scheduled.ScheduleCron, Expr: c["expr"]}
	case string(scheduled.ScheduleEvery):
		var secs int64
		secs = parseSeconds(c["every_seconds"])
		if secs <= 0 {
			store.Cancel(pending.ID)
			return "I couldn't schedule that — the timing wasn't clear. Try again?", true
		}
		sched = scheduled.Schedule{Kind: scheduled.ScheduleEvery, Every: time.Duration(secs) * time.Second}
	case string(scheduled.ScheduleAt):
		at, err := time.Parse(time.RFC3339, c["at"])
		if err != nil {
			store.Cancel(pending.ID)
			return "I couldn't schedule that — the timing wasn't clear. Try again?", true
		}
		sched = scheduled.Schedule{Kind: scheduled.ScheduleAt, At: &at}
	default:
		store.Cancel(pending.ID)
		return "I couldn't schedule that — the timing wasn't clear. Try again?", true
	}
	schedStore := scheduled.NewStore(al.db.DB)
	if err := schedStore.InitSchema(); err != nil {
		return "", false
	}
	svc, err := routines.New(al.db.DB, schedStore)
	if err != nil {
		return "", false
	}
	ghostID, ownerID := routineIDs(al.workspace)
	name := ifEmpty(c["name"], "Routine")
	instruction := ifEmpty(c["instruction"], pending.Intent)
	// Duplicate prevention: an identical active routine (same instruction,
	// same schedule) must not stack. The user repeating the request gets an
	// honest "already scheduled" instead of a second identical routine.
	if existing := findMatchingRoutine(svc, ghostID, instruction, sched); existing != nil {
		store.Complete(pending.ID)
		return "You already have that routine (" + existing.Name + "). I didn't create a duplicate.", true
	}
	r, err := svc.Create(ghostID, ownerID, name, instruction, tz, sched, nil)
	if err != nil {
		store.Cancel(pending.ID)
		return "I couldn't create that routine. Nothing was scheduled.", true
	}
	// Delivery target: the routine reports back where it was created.
	// Without this the run completes but nobody hears about it.
	func() {
		schedStore := scheduled.NewStore(al.db.DB)
		item, gerr := schedStore.Get(r.ID)
		if gerr != nil {
			return
		}
		if item.Channel == "" && msg.Channel != "" {
			item.Channel = msg.Channel
		}
		if item.ChatID == "" && msg.ChatID != "" {
			item.ChatID = msg.ChatID
		}
		_ = schedStore.Update(item)
	}()
	store.Complete(pending.ID)
	if al.governance != nil && al.governance.Events != nil {
		al.governance.Events.Publish(&cevents.Event{
			Type: cevents.RoutineCreated, RequestID: msg.Metadata["request_id"],
			SessionID: msg.SessionKey, GhostID: al.governance.GhostID,
			AgentID: al.governance.AgentID, RoutineID: r.ID,
			Payload: map[string]interface{}{"summary": name + " scheduled"},
		})
	}
	clause := c["schedule_text"]
	if clause == "" {
		clause = "on schedule"
	}
	return "Done. I'll remind you to " + routineTask(instruction) + " " + clause + ".", true
}

func ifEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func truncateRoutine(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = s[:n-1] + "…"
	}
	// The routine name is owner-facing (Things list). Present it as a title:
	// "prepare my brief" reads as a fragment, "Prepare my brief" reads as a
	// task. Only the first rune is touched.
	if s != "" {
		r := []rune(s)
		if r[0] >= 'a' && r[0] <= 'z' {
			r[0] = r[0] - 32
		}
		s = string(r)
	}
	return s
}

func routineTask(instruction string) string {
	trimmed := strings.TrimSpace(instruction)
	lower := strings.ToLower(trimmed)
	if rest, ok := strings.CutPrefix(lower, "remind me to "); ok {
		return strings.TrimSpace(trimmed[len(trimmed)-len(rest):])
	}
	return trimmed
}

func parseSeconds(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func routineTimezone(msg bus.InboundMessage) string {
	if tz := strings.TrimSpace(msg.Metadata["timezone"]); tz != "" {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	return "UTC"
}

func routineIDs(workspace string) (ghostID, ownerID string) {
	if id, err := ghoststate.LoadIdentity(workspace); err == nil && id != nil {
		return id.GhostID, id.OwnerName
	}
	return "ghost-local", "owner"
}

// findMatchingRoutine returns an active routine with the same instruction
// AND the same effective schedule, or nil. Prevents duplicate stacking when
// the user repeats an identical routine request.
func findMatchingRoutine(svc *routines.Service, ghostID, instruction string, sched scheduled.Schedule) *routines.Routine {
	for _, r := range svc.List(ghostID, 100) {
		if r.Status != routines.StatusActive && r.Status != routines.StatusPaused {
			continue
		}
		if strings.TrimSpace(r.Instruction) != strings.TrimSpace(instruction) {
			continue
		}
		switch sched.Kind {
		case scheduled.ScheduleCron:
			if r.ScheduleKind == "cron" && strings.TrimSpace(r.ScheduleExpr) == strings.TrimSpace(sched.Expr) {
				return r
			}
		case scheduled.ScheduleEvery:
			if r.ScheduleKind == "every" && r.ScheduleEverySecs == int64(sched.Every/time.Second) {
				return r
			}
		case scheduled.ScheduleAt:
			if r.ScheduleKind == "at" && sched.At != nil && r.ScheduleExpr == sched.At.Format(time.RFC3339) {
				return r
			}
		}
	}
	return nil
}
