package agent

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/activity"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/commitments"
	"github.com/ianclemence/ghost/pkg/hardware"
	"github.com/ianclemence/ghost/pkg/ideas"
	"github.com/ianclemence/ghost/pkg/modes"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/proactive"
	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/watch"
)

// Deterministic state answers.
//
// Ghost already owns the authoritative answer to a large class of questions —
// what is scheduled, what needs the owner, what failed, how much disk is left.
// Sending those through a model costs a cloud round trip (~0.9 s measured) plus
// a ~22k-token prompt, and adds a chance of the model paraphrasing runtime
// state incorrectly.
//
// Every renderer here is STATE-BOUND: it prints only fields read from
// authoritative stores. If a store is unwired, a question is ambiguous, or the
// data cannot be read, the renderer returns handled=false and the message
// continues down the normal path. This path never guesses and never invents a
// name, id, date, count, or completion state — an earlier evaluation caught
// exactly that failure in runtime-authored text.

var (
	reminderQueryRE = regexp.MustCompile(`(?i)\b(` +
		`what (?:reminders?|alarms?)|any (?:reminders?|alarms?)|my (?:reminders?|alarms?)|` +
		`(?:check|list|show|see) (?:my )?(?:reminders?|alarms?|schedule)|` +
		`what'?s (?:on|scheduled) (?:today|tonight|tomorrow|this week)|` +
		`next reminder|overdue reminders?|did i miss (?:any )?reminders?|` +
		`what do i have scheduled` +
		`)\b`)

	proposalQueryRE = regexp.MustCompile(`(?i)\b(` +
		`what needs me|anything needs me|do i have anything waiting|` +
		`any (?:proposals?|suggestions?)|what (?:did you|have you) notice|` +
		`what should i look at|anything for me` +
		`)\b`)

	approvalQueryRE = regexp.MustCompile(`(?i)\b(` +
		`(?:anything|what|is there anything) (?:waiting )?for approval|` +
		`pending approvals?|do i need to approve|` +
		`(?:any )?approvals? (?:waiting|pending)|what'?s waiting for (?:me|approval)` +
		`)\b`)

	routineQueryRE = regexp.MustCompile(`(?i)\b(` +
		`(?:is|are) my .{0,20}routines? (?:ok|okay|fine|healthy|running)|` +
		`(?:are|is) (?:my |the )?routines? (?:ok|okay|fine|healthy|alright)|` +
		`my routines?|what routines|which routin\w+ (?:failed|is failing)|what failed|` +
		`routine status` +
		`)\b`)

	jobQueryRE = regexp.MustCompile(`(?i)\b(` +
		`(?:what|any) (?:tasks?|jobs?) (?:are )?(?:overdue|failed|waiting|stuck|running)|` +
		`(?:overdue|failed|stuck) (?:tasks?|jobs?)|` +
		`what'?s waiting on me|pending tasks?` +
		`)\b`)

	activityQueryRE = regexp.MustCompile(`(?i)\b(` +
		`what did you (?:just )?do|what have you been (?:doing|up to)|` +
		`what are you doing|recent activity|what'?s happening|` +
		`what have you done|your last actions?` +
		`)\b`)

	healthQueryRE = regexp.MustCompile(`(?i)\b(` +
		`(?:are|is) (?:you|ghost) (?:ok|okay|healthy|fine|alright|up)|` +
		`ghost health|system health|how healthy|` +
		`(?:how much|what'?s) (?:disk|space|storage) (?:is )?(?:left|free|available)|` +
		`free (?:disk|space|storage)|disk (?:space|usage)|storage (?:left|usage)|` +
		`how much (?:memory|ram)|memory usage|cpu (?:usage|load)|` +
		`ghost status|system status` +
		`)\b`)

	modelQueryRE = regexp.MustCompile(`(?i)\b(` +
		`what model (?:are you|do you use|is (?:active|running|set))|` +
		`which model|what (?:ai|llm) (?:are you|is running)|` +
		`are you (?:local|cloud|running locally)|what mode` +
		`)\b`)

	proactivePolicyQueryRE = regexp.MustCompile(`(?i)\b(` +
		`is (?:proactive|proactivity|proactive mode|suggestions?) (?:on|off|enabled|disabled|active)|` +
		`are (?:proactive )?suggestions? (?:on|off|enabled|disabled)|` +
		`(?:what are|show me) (?:my )?quiet hours|when are quiet hours` +
		`)\b`)

	commitmentQueryRE = regexp.MustCompile(`(?i)\b(` +
		`what (?:promises|commitments)|my (?:promises|commitments)|` +
		`what did i promise|anything i (?:said i'?d|promised)|what am i (?:supposed|meant) to do` +
		`)\b`)

	watchQueryRE = regexp.MustCompile(`(?i)\b(` +
		`what am i (?:watching|tracking)|what (?:are|is) (?:you|ghost) watching|` +
		`my watches|list watches|show (?:me )?my watches|any watches|` +
		`which flights? (?:are|am) (?:you|ghost) watching|am i watching` +
		`)\b`)
)

// compoundAskRE marks a message that asks for more than one thing or asks for
// work to be done, so the state fast path must not swallow it.
var compoundAskRE = regexp.MustCompile(`(?i)\b(and|then|also|plus|afterwards?|run|execute|command|commands|script|uptime)\b`)

// simpleStateQuestion reports whether a message is one short, plain question.
func simpleStateQuestion(lower string) bool {
	return len(strings.Fields(lower)) <= 14 &&
		strings.Count(lower, "?") <= 1 &&
		!compoundAskRE.MatchString(lower)
}

// tryStateQueryTurn answers questions whose answer is already authoritative
// runtime state. Zero model calls, zero embeddings.
func (al *AgentLoop) tryStateQueryTurn(msg, session string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(msg))
	if lower == "" {
		return "", false
	}
	// The fast path answers one plain question from runtime state. A message
	// that asks for more (a second question, "run the commands", "and then…")
	// would get only its first half answered, so it goes to the model.
	if !simpleStateQuestion(lower) {
		return "", false
	}
	switch {
	case reminderQueryRE.MatchString(lower):
		return al.renderReminders(session)
	case proposalQueryRE.MatchString(lower):
		return al.renderProposals()
	case approvalQueryRE.MatchString(lower):
		return al.renderApprovals()
	case routineQueryRE.MatchString(lower):
		return al.renderRoutines()
	case jobQueryRE.MatchString(lower):
		return al.renderJobs()
	case activityQueryRE.MatchString(lower):
		return al.renderActivity()
	case healthQueryRE.MatchString(lower):
		return al.renderHealth()
	case modelQueryRE.MatchString(lower):
		return al.renderModelState()
	case proactivePolicyQueryRE.MatchString(lower):
		return al.renderProactivePolicy()
	case commitmentQueryRE.MatchString(lower):
		return al.renderCommitments()
	case watchQueryRE.MatchString(lower):
		return al.renderWatches()
	}
	return "", false
}

// renderReminders reuses the schedule tool's own list renderer, so a
// deterministic answer and a model-driven one can never drift apart. It is a
// read: no capability is executed.
func (al *AgentLoop) renderReminders(session string) (string, bool) {
	if al.schedSvc == nil || al.tools == nil {
		return "", false
	}
	if _, ok := al.tools.Get("schedule"); !ok {
		return "", false
	}
	return al.execDeterministicTool("schedule", map[string]interface{}{"action": "list"}, session)
}

// renderProposals lists open opportunities from the proposal store.
func (al *AgentLoop) renderProposals() (string, bool) {
	if al.workspace == "" {
		return "", false
	}
	store, err := ideas.New(al.workspace)
	if err != nil {
		return "", false
	}
	open, err := store.Open(10)
	if err != nil {
		return "", false
	}
	if len(open) == 0 {
		return "Nothing needs you right now.", true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d waiting on you:\n", len(open))
	for _, i := range open {
		line := strings.TrimSpace(i.Body)
		if line == "" {
			line = strings.TrimSpace(i.Title)
		}
		fmt.Fprintf(&b, "- %s\n", line)
		if i.Plan != nil && strings.TrimSpace(i.Plan.Describe) != "" {
			fmt.Fprintf(&b, "  (offered action: %s)\n", i.Plan.Describe)
		}
	}
	return strings.TrimRight(b.String(), "\n"), true
}

// renderApprovals lists live approval requests. Titles come from the broker's
// own card projection, so no request detail is invented here.
func (al *AgentLoop) renderApprovals() (string, bool) {
	if al.governance == nil || al.governance.Broker == nil {
		return "", false
	}
	reqs := al.governance.Broker.Requests(permissions.StatusPending, 10)
	if len(reqs) == 0 {
		return "Nothing is waiting for your approval.", true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d waiting for your approval:\n", len(reqs))
	for _, r := range reqs {
		title := r.Capability
		if card, ok := r.Card(); ok && strings.TrimSpace(card.Title) != "" {
			title = card.Title
		}
		expires := ""
		if !r.ExpiresAt.IsZero() {
			expires = " — expires " + r.ExpiresAt.In(time.Local).Format("15:04")
		}
		fmt.Fprintf(&b, "- %s%s\n", title, expires)
	}
	return strings.TrimRight(b.String(), "\n"), true
}

// renderRoutines reports routine health from the routine overlay and its run
// history. No routine is executed.
func (al *AgentLoop) renderRoutines() (string, bool) {
	if al.routineSvc == nil {
		return "", false
	}
	list := al.routineSvc.List(al.ghostID(), 50)
	if len(list) == 0 {
		return "You don't have any routines yet.", true
	}
	var failing, waiting, active []string
	for _, r := range list {
		if r == nil {
			continue
		}
		name := displayName(r.Name, r.ID)
		switch r.Status {
		case routines.StatusFailed:
			failing = append(failing, name)
		case routines.StatusWaiting:
			waiting = append(waiting, name)
		case routines.StatusActive:
			active = append(active, name)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d routine(s): %d active", len(list), len(active))
	if len(failing) > 0 {
		fmt.Fprintf(&b, ", %d failed (%s)", len(failing), strings.Join(failing, ", "))
	}
	if len(waiting) > 0 {
		fmt.Fprintf(&b, ", %d waiting on you (%s)", len(waiting), strings.Join(waiting, ", "))
	}
	b.WriteString(".")
	if len(failing) == 0 && len(waiting) == 0 {
		b.WriteString(" Nothing is wrong.")
	}
	return b.String(), true
}

// renderJobs reports durable task state from the job store.
func (al *AgentLoop) renderJobs() (string, bool) {
	if al.jobs == nil {
		return "", false
	}
	jobs, err := al.jobs.List("")
	if err != nil {
		return "", false
	}
	var stuck, failed []string
	for _, j := range jobs {
		switch j.Status {
		case "waiting_for_user", "waiting_for_permission", "paused":
			stuck = append(stuck, fmt.Sprintf("%s (%s)", j.Kind, j.Status))
		case "failed", "interrupted":
			failed = append(failed, j.Kind)
		}
	}
	if len(stuck) == 0 && len(failed) == 0 {
		return "No tasks are stuck or failing.", true
	}
	var parts []string
	if len(stuck) > 0 {
		parts = append(parts, fmt.Sprintf("%d waiting on you: %s", len(stuck), strings.Join(stuck, ", ")))
	}
	if len(failed) > 0 {
		parts = append(parts, fmt.Sprintf("%d failed: %s", len(failed), strings.Join(failed, ", ")))
	}
	return strings.Join(parts, ". ") + ".", true
}

// renderActivity reports what Ghost recently did, projected from the canonical
// event stream through the same chip projection the console uses.
func (al *AgentLoop) renderActivity() (string, bool) {
	if al.governance == nil || al.governance.Events == nil {
		return "", false
	}
	events := al.governance.Events.Recent(120, cevents.Filter{})
	// Same honesty rule the console gets: a request the owner has already
	// answered is not something still waiting on them. Without this, a
	// resolved approval survives in the recent trail and Ghost would report
	// itself as waiting when it is not.
	events = activity.WithoutAnsweredApprovals(events, permissions.OpenRequests(al.governance.Broker))
	type row struct {
		at    time.Time
		title string
		state string
	}
	var rows []row
	for _, e := range events {
		if e == nil {
			continue
		}
		chip, ok := activity.Project(e)
		if !ok {
			continue
		}
		rows = append(rows, row{at: e.Timestamp, title: chip.Title, state: string(chip.State)})
	}
	if len(rows) == 0 {
		return "I haven't done anything yet.", true
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].at.After(rows[j].at) })
	if len(rows) > 6 {
		rows = rows[:6]
	}
	var b strings.Builder
	b.WriteString("My last actions:\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "- %s (%s, %s)\n", r.title, r.state, r.at.In(time.Local).Format("15:04"))
	}
	return strings.TrimRight(b.String(), "\n"), true
}

// renderHealth reports real device state from the same pressure snapshot the
// rest of the runtime uses.
func (al *AgentLoop) renderHealth() (string, bool) {
	path := al.workspace
	if path == "" {
		path = "."
	}
	snap := hardware.Snapshot(path)
	head := fmt.Sprintf("Memory is %s and storage is %s", snap.Memory, snap.Storage)
	if snap.Memory == snap.Storage {
		head = fmt.Sprintf("Memory and storage are both %s", snap.Memory)
	}
	parts := []string{head + fmt.Sprintf(": %d MB of %d MB RAM available", snap.MemAvailableMB, snap.MemTotalMB)}
	if snap.DiskTotalGB > 0 {
		parts = append(parts, fmt.Sprintf("%d GB of %d GB disk free", snap.DiskFreeGB, snap.DiskTotalGB))
	}
	if up := uptimeText(); up != "" {
		parts = append(parts, "up "+up)
	}
	return strings.Join(parts, ", ") + ".", true
}

// uptimeText reads how long this machine has been running, in plain words
// ("3 days, 4 hours"), or "" where the system doesn't say.
func uptimeText() string {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return ""
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return ""
	}
	secs, err := strconv.ParseFloat(f[0], 64)
	if err != nil || secs < 0 {
		return ""
	}
	d := time.Duration(secs) * time.Second
	days, hours, mins := int(d.Hours())/24, int(d.Hours())%24, int(d.Minutes())%60
	switch {
	case days > 0:
		return fmt.Sprintf("%d day%s, %d hour%s", days, plural(days), hours, plural(hours))
	case hours > 0:
		return fmt.Sprintf("%d hour%s, %d minute%s", hours, plural(hours), mins, plural(mins))
	}
	return fmt.Sprintf("%d minute%s", mins, plural(mins))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// renderModelState reports the active model and mode from live configuration.
func (al *AgentLoop) renderModelState() (string, bool) {
	model := strings.TrimSpace(al.model)
	if model == "" {
		return "", false
	}
	mode := string(modes.Resolve(al.workspace, al.hasCloudKey()))
	return fmt.Sprintf("I'm running %s in %s mode.", model, mode), true
}

// renderProactivePolicy reports the parsed proactivity contract.
func (al *AgentLoop) renderProactivePolicy() (string, bool) {
	if al.workspace == "" {
		return "", false
	}
	p := proactive.Load(al.workspace)
	if !p.Enabled {
		return "Proactive suggestions are off.", true
	}
	return fmt.Sprintf("Proactive suggestions are on — quiet hours %s–%s, up to %d check-ins a day.",
		clockLabel(p.QuietStart), clockLabel(p.QuietEnd), p.MaxPushesPerDay), true
}

// renderCommitments reports the promise ledger.
func (al *AgentLoop) renderCommitments() (string, bool) {
	store, err := al.commitmentStoreFor()
	if err != nil {
		return "", false
	}
	list, err := store.List()
	if err != nil {
		return "", false
	}
	var open []commitments.Commitment
	for _, c := range list {
		if c.Status == commitments.StatusOpen || c.Status == commitments.StatusBlocked {
			open = append(open, c)
		}
	}
	if len(open) == 0 {
		return "You have no open promises on file.", true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d open promise(s):\n", len(open))
	for _, c := range open {
		when := ""
		if c.DueAt != nil {
			when = " — due " + c.DueAt.In(time.Local).Format("Mon 2 Jan 15:04")
		}
		fmt.Fprintf(&b, "- %s%s\n", c.Text, when)
	}
	return strings.TrimRight(b.String(), "\n"), true
}

// renderWatches reports the watchlist from the durable store. Every line
// is projected from stored state (status, next check, delivered changes) —
// this renderer never remembers what it thinks is being watched.
func (al *AgentLoop) renderWatches() (string, bool) {
	if al.workspace == "" {
		return "", false
	}
	store, err := al.watchStoreFor()
	if err != nil {
		return "", false
	}
	list, err := store.List()
	if err != nil {
		return "", false
	}
	// Show what is being watched plus anything that stopped with a problem
	// the owner should know about; settled watches (cancelled, expired)
	// are history and would only clutter the answer.
	var shown []watch.Watch
	for _, w := range list {
		if w.Live() || w.Status == watch.StatusFailed {
			shown = append(shown, w)
		}
	}
	return watch.RenderList(shown, time.Now()), true
}

func clockLabel(mins int) string {
	if mins < 0 {
		mins = 0
	}
	return fmt.Sprintf("%02d:%02d", (mins/60)%24, mins%60)
}
