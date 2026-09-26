package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/goals"
	"github.com/ianclemence/ghost/pkg/ideas"
	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
	"github.com/ianclemence/ghost/pkg/tasks"
)

// Proactive observation: the grounding half of the proactive engine. Every
// Observation is derived from rows in a real subsystem — the scheduler, the
// routine sidecar, the goal store, the durable job store. Nothing here reads
// model output, and nothing here guesses. If a subsystem is unwired, its
// observations simply do not exist.
//
// The runtime never asks a model "what should I suggest?". Observations are
// cheap deterministic queries; the model is only involved later, if at all,
// when a proposal the owner approved actually runs.

// Observation tuning. These are policy knobs for what counts as "worth
// noticing", not safety controls: the safety controls are the broker and the
// permission broker's risk classes.
const (
	// overdueGrace keeps a just-due reminder from being called overdue while
	// the scheduler is legitimately mid-tick.
	overdueGrace = 15 * time.Minute
	// stalledGoalAfter is how long a usable goal may go without a progress
	// note before Ghost offers to do something about it.
	stalledGoalAfter = 48 * time.Hour
	// staleRoutineAfter is how long an active routine may go unrrun before
	// Ghost offers to pause it (mirrors the existing idea rule).
	staleRoutineAfter = 30 * 24 * time.Hour
)

// observationLimit bounds how many rows each collector examines per
// evaluation. The proactive layer must never turn a heartbeat into a full
// table scan on a Raspberry Pi.
const observationLimit = 200

// CollectObservations gathers grounded facts worth considering. It is nil-safe
// per subsystem: an unwired runtime simply observes less.
func (al *AgentLoop) CollectObservations(now time.Time) []ideas.Observation {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var out []ideas.Observation
	out = append(out, al.observeReminders(now)...)
	out = append(out, al.observeRoutines(now)...)
	out = append(out, al.observeGoals(now)...)
	out = append(out, al.observeTasks(now)...)
	return out
}

// observeReminders finds scheduled reminders and tasks that did not reach the
// owner: still pending well past their time, or recorded as failed. These are
// exactly the cases where Ghost's promise ("I'll remind you") was broken, so
// noticing them is the product working, not noise.
func (al *AgentLoop) observeReminders(now time.Time) []ideas.Observation {
	if al.schedSvc == nil {
		return nil
	}
	items, err := al.schedSvc.ListItems("", "", observationLimit)
	if err != nil {
		return nil
	}
	var out []ideas.Observation
	for _, it := range items {
		if it == nil {
			continue
		}
		if it.Type != scheduled.TypeReminder && it.Type != scheduled.TypeTask {
			continue
		}
		switch it.State {
		case scheduled.StateScheduled, scheduled.StateDue:
			if it.NextRunAt == nil || !it.NextRunAt.Before(now.Add(-overdueGrace)) {
				continue
			}
			// A recurring item is late for one occurrence, not broken: the
			// next run will catch it. Only one-shots are "never delivered".
			if it.Schedule.Kind != scheduled.ScheduleAt {
				continue
			}
			out = append(out, ideas.Observation{
				Kind:    ideas.ObsReminderOverdue,
				Subject: it.ID,
				Summary: fmt.Sprintf("Your reminder %q was due %s and never reached you",
					itemTitle(it), humanAge(now, *it.NextRunAt)),
				Detail: fmt.Sprintf("It was due %s and is still waiting to run.",
					it.NextRunAt.In(now.Location()).Format("Mon 15:04")),
				At:         now,
				Evidence:   []ideas.Source{{Kind: ideas.SourceSchedule, Ref: it.ID, Excerpt: "due " + it.NextRunAt.UTC().Format(time.RFC3339)}},
				StateVer:   ideas.StateVer(it.ID, string(it.State), it.NextRunAt.UTC().Format(time.RFC3339), fmt.Sprint(it.RunCount)),
				Actionable: true,
				Priority:   8,
				Confidence: 0.95,
			})
		case scheduled.StateFailed:
			hist, _ := al.schedSvc.GetHistory(it.ID, 1)
			when := ""
			if it.LastRunAt != nil {
				when = " on " + it.LastRunAt.In(now.Location()).Format("Mon 15:04")
			}
			detail := strings.TrimSpace(it.LastError)
			out = append(out, ideas.Observation{
				Kind:    ideas.ObsReminderFailed,
				Subject: it.ID,
				Summary: fmt.Sprintf("Your reminder %q failed%s", itemTitle(it), when),
				Detail:  failureDetail(detail, hist),
				At:      now,
				Evidence: []ideas.Source{{
					Kind: ideas.SourceSchedule, Ref: it.ID,
					Excerpt: "state=failed" + failureExcerpt(detail),
				}},
				StateVer:   ideas.StateVer(it.ID, string(it.State), fmt.Sprint(it.RunCount), detail),
				Actionable: true,
				Priority:   8,
				Confidence: 0.9,
			})
		}
	}
	return out
}

// observeRoutines notices routines that need the owner: failing runs, runs
// parked waiting for approval, and routines that quietly stopped running.
func (al *AgentLoop) observeRoutines(now time.Time) []ideas.Observation {
	if al.routineSvc == nil {
		return nil
	}
	list := al.routineSvc.List(al.ghostID(), observationLimit)
	var out []ideas.Observation
	for _, r := range list {
		if r == nil {
			continue
		}
		name := displayName(r.Name, r.ID)
		switch r.Status {
		case routines.StatusWaiting:
			out = append(out, ideas.Observation{
				Kind:    ideas.ObsRoutineWaiting,
				Subject: r.ID,
				Summary: fmt.Sprintf("Your routine %q is waiting for your approval to continue", name),
				Detail:  "Its last run stopped on a permission request.",
				At:      now,
				Evidence: []ideas.Source{{
					Kind: ideas.SourceRoutine, Ref: r.ID,
					Excerpt: "status=waiting",
				}},
				StateVer:   ideas.StateVer(r.ID, string(r.Status)),
				Actionable: true,
				Priority:   7,
				Confidence: 0.9,
			})
		case routines.StatusFailed:
			out = append(out, al.failedRoutineObservation(r, now))
		default:
			if al.routineRecentWaiting(r.ID) {
				out = append(out, ideas.Observation{
					Kind:    ideas.ObsRoutineWaiting,
					Subject: r.ID,
					Summary: fmt.Sprintf("Your routine %q asked for approval on its last run and is still waiting", name),
					Detail:  "Its most recent recorded run ended waiting on a permission request.",
					At:      now,
					Evidence: []ideas.Source{{
						Kind: ideas.SourceRoutine, Ref: r.ID,
						Excerpt: "latest run status=waiting",
					}},
					StateVer:   ideas.StateVer(r.ID, "waiting", r.UpdatedAt.UTC().Format(time.RFC3339)),
					Actionable: true,
					Priority:   7,
					Confidence: 0.85,
				})
				continue
			}
			// "Keeps failing" must mean the latest run failed. A routine that
			// failed yesterday and succeeded since has recovered, and Ghost
			// must not keep offering to fix it.
			if n := routineRecentErrors(al.schedSvc, r.ID); n > 0 && al.routineLastRunFailed(r.ID) {
				out = append(out, al.failedRoutineObservation(r, now))
				continue
			}
			if r.Status == routines.StatusActive && r.LastRun != nil && now.Sub(*r.LastRun) >= staleRoutineAfter {
				days := int(now.Sub(*r.LastRun).Hours() / 24)
				out = append(out, ideas.Observation{
					Kind:    ideas.ObsRoutineStale,
					Subject: r.ID,
					Summary: fmt.Sprintf("Your routine %q hasn't run in %d days but is still scheduled", name, days),
					Detail:  fmt.Sprintf("Last run %s.", r.LastRun.In(now.Location()).Format("2006-01-02")),
					At:      now,
					Evidence: []ideas.Source{{
						Kind: ideas.SourceRoutine, Ref: r.ID + ":stale",
						Excerpt: "last run " + r.LastRun.In(now.Location()).Format("2006-01-02"),
					}},
					StateVer:   ideas.StateVer(r.ID, "stale", r.LastRun.UTC().Format("2006-01-02")),
					Actionable: true,
					Priority:   7,
					Confidence: 0.75,
				})
			}
		}
	}
	return out
}

func (al *AgentLoop) failedRoutineObservation(r *routines.Routine, now time.Time) ideas.Observation {
	name := displayName(r.Name, r.ID)
	detail := strings.TrimSpace(routineLastError(al.schedSvc, r.ID))
	fails := routineRecentErrors(al.schedSvc, r.ID)
	// The latest recorded run is part of the state digest: once a later run
	// succeeds, this proposal's basis is gone and it must be voided rather
	// than left approvable.
	latestRun := ""
	if al.schedSvc != nil {
		if hist, err := al.schedSvc.GetHistory(r.ID, 1); err == nil && len(hist) > 0 && hist[0] != nil {
			latestRun = hist[0].ExecutionID
		}
	}
	summary := fmt.Sprintf("Your routine %q failed", name)
	if r.LastRun != nil {
		summary += " on " + r.LastRun.In(now.Location()).Format("Mon 15:04")
	}
	if fails > 1 {
		summary += fmt.Sprintf(" — %d recent failures in a row", fails)
	}
	out := ideas.Observation{
		Kind:    ideas.ObsRoutineFailed,
		Subject: r.ID,
		Summary: summary,
		Detail:  failureDetail(detail, nil),
		At:      now,
		Evidence: []ideas.Source{{
			Kind: ideas.SourceRoutine, Ref: r.ID + ":failed:" + latestRun,
			Excerpt: fmt.Sprintf("recent failures=%d", fails) + failureExcerpt(detail),
		}},
		StateVer:   ideas.StateVer(r.ID, "failed", fmt.Sprint(fails), latestRun, detail),
		Actionable: true,
		Priority:   8,
		Confidence: 0.85,
	}
	return out
}

// observeGoals notices usable goals whose stewardship went quiet. A goal is
// the owner's standing intent; letting one drift with no progress and no
// mention is the opposite of "removes mental overhead".
func (al *AgentLoop) observeGoals(now time.Time) []ideas.Observation {
	if al.workspace == "" {
		return nil
	}
	list, err := goals.NewStore(al.workspace).List(now)
	if err != nil {
		return nil
	}
	var out []ideas.Observation
	seen := 0
	for _, g := range list {
		if !g.Usable(now) || len(g.Progress) == 0 {
			continue
		}
		last := g.Progress[len(g.Progress)-1].At
		if now.Sub(last) < stalledGoalAfter {
			continue
		}
		if seen >= 20 {
			break
		}
		seen++
		days := int(now.Sub(last).Hours() / 24)
		out = append(out, ideas.Observation{
			Kind:    ideas.ObsGoalStalled,
			Subject: g.ID,
			Summary: fmt.Sprintf("Your goal %q hasn't had progress in %d days", g.Text, days),
			Detail:  fmt.Sprintf("Last progress note %s.", last.In(now.Location()).Format("2006-01-02")),
			At:      now,
			Evidence: []ideas.Source{{
				Kind: ideas.SourceGoal, Ref: g.ID,
				Excerpt: "last progress " + last.In(now.Location()).Format("2006-01-02"),
			}},
			StateVer:   ideas.StateVer(g.ID, last.UTC().Format(time.RFC3339), fmt.Sprint(len(g.Progress))),
			Actionable: true,
			Priority:   7,
			Confidence: 0.7,
		})
	}
	return out
}

// observeTasks notices durable jobs parked waiting on the owner. These are the
// runtime's own long-running pieces of work; when one stops because it needs a
// human, that is precisely what "Ghost needs you" means.
func (al *AgentLoop) observeTasks(now time.Time) []ideas.Observation {
	if al.jobs == nil {
		return nil
	}
	var out []ideas.Observation
	for _, st := range []tasks.Status{tasks.StatusWaitingUser, tasks.StatusWaitingPermission} {
		jobs, err := al.jobs.List(st)
		if err != nil {
			continue
		}
		for i := range jobs {
			j := jobs[i]
			if len(out) >= 20 {
				break
			}
			summary := fmt.Sprintf("A %s task has been waiting for you", j.Kind)
			excerpt := string(j.Status)
			if len(j.Checkpoints) > 0 {
				if cp := strings.TrimSpace(j.Checkpoints[len(j.Checkpoints)-1]); cp != "" {
					excerpt += " at " + cp
				}
			}
			out = append(out, ideas.Observation{
				Kind:    ideas.ObsTaskOverdue,
				Subject: j.ID,
				Summary: summary,
				Detail:  excerpt,
				At:      now,
				Evidence: []ideas.Source{{
					Kind: ideas.SourceTask, Ref: j.ID, Excerpt: excerpt,
				}},
				StateVer:   ideas.StateVer(j.ID, string(j.Status), fmt.Sprint(j.UpdatedAt)),
				Actionable: true,
				Priority:   8,
				Confidence: 0.9,
			})
		}
	}
	return out
}

func itemTitle(it *scheduled.ScheduledItem) string {
	if t := strings.TrimSpace(it.Title); t != "" {
		return t
	}
	if c := strings.TrimSpace(it.Action.Content); c != "" {
		return truncateReason(c, 60)
	}
	return it.ID
}

func failureDetail(detail string, hist []*scheduled.ExecutionRecord) string {
	if detail != "" {
		return "Last error: " + truncateReason(detail, 200)
	}
	if len(hist) > 0 && hist[0] != nil && hist[0].Error != "" {
		return "Last error: " + truncateReason(hist[0].Error, 200)
	}
	return "Its last run did not complete."
}

func failureExcerpt(detail string) string {
	if detail == "" {
		return ""
	}
	return " error=" + truncateReason(detail, 80)
}

func humanAge(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}

// routineLastRunFailed reports whether the most recent recorded run failed.
// It is the honest reading of "keeps failing": a routine that failed and has
// since succeeded has recovered.
func (al *AgentLoop) routineLastRunFailed(id string) bool {
	if al.schedSvc == nil {
		return false
	}
	hist, err := al.schedSvc.GetHistory(id, 1)
	if err != nil || len(hist) == 0 || hist[0] == nil {
		return false
	}
	return hist[0].Status == "error" || hist[0].Status == "missed"
}
