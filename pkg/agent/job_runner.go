package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/tasks"
	"github.com/ianclemence/ghost/pkg/tools"
)

// subagentJobKind is the one job kind this runtime knows how to execute.
const subagentJobKind = "subagent"

// wireJobRunner turns the durable job store into a system that actually
// runs things. Three things happen here and each one matters on its own:
//
//   - the spawn tool is pointed at the store, so a background task is
//     written down BEFORE it runs and a restart finds it;
//   - the handler that executes such a job is registered, so a row means
//     work rather than a plan to do work;
//   - everything a crash left interrupted is re-queued before the loop
//     starts, so the first tick after a boot sees it.
func (al *AgentLoop) wireJobRunner(sm *tools.SubagentManager) {
	r := tasks.NewRunner(al.jobs)
	r.SetLogf(func(format string, args ...interface{}) {
		logger.WarnCF("agent", "job runner: "+fmt.Sprintf(format, args...), map[string]interface{}{})
	})
	r.Register(subagentJobKind, al.runSubagentJob)
	r.SetSettleHook(al.announceJobSettled)
	al.jobRunner = r

	sm.SetDurableSpawner(func(req tools.SpawnRequest) (string, error) {
		j, err := al.jobs.Create(subagentJobKind, req.SessionKey, map[string]interface{}{
			"task":     req.Task,
			"label":    req.Label,
			"channel":  req.Channel,
			"chat_id":  req.ChatID,
			"asked_at": time.Now().Unix(),
		})
		if err != nil {
			return "", err
		}
		// The job is already due; run it now rather than at the next tick.
		r.Wake()
		return j.ID, nil
	})

	if n, err := r.Recover(); err != nil {
		logger.WarnCF("agent", "durable job recovery failed",
			map[string]interface{}{"error": err.Error()})
	} else if n > 0 {
		logger.InfoCF("agent", "durable jobs requeued after restart",
			map[string]interface{}{"count": n})
	}
	r.Start(al.shutdownCtx)
}

// runSubagentJob is the handler the runner calls for a recorded spawn. Its
// only job is to translate what happened into the store's vocabulary:
// finished, waiting for the owner, or worth another attempt.
func (al *AgentLoop) runSubagentJob(ctx context.Context, j tasks.Job) error {
	if al.subagents == nil {
		return tasks.Fatal("this runtime cannot execute background jobs")
	}
	task, _ := j.Payload["task"].(string)
	if strings.TrimSpace(task) == "" {
		return tasks.Fatal("the recorded job carries no task to run")
	}
	label, _ := j.Payload["label"].(string)
	channel, _ := j.Payload["channel"].(string)
	chatID, _ := j.Payload["chat_id"].(string)

	// Progress is measured against one attempt's tool budget, so "how far
	// through" means the same thing to the record as it does to the run.
	budget := al.subagents.IterationBudget()
	if budget <= 0 {
		budget = 1
	}
	done := len(j.Checkpoints)

	err := al.subagents.RunDurable(ctx, tools.DurableAttempt{
		JobID:   j.ID,
		Task:    task,
		Label:   label,
		Channel: channel,
		ChatID:  chatID,
		// What earlier attempts confirmed. An interrupted first attempt
		// has nothing, and starting at the beginning is the only honest
		// place to start from.
		Resume: strings.Join(j.Checkpoints, "\n"),
		Checkpoint: func(line string) {
			p := float64(done+1) / float64(budget)
			if p > 0.95 {
				p = 0.95 // progress, not a promise of completion
			}
			if _, perr := al.jobs.Progress(j.ID, p, line); perr != nil {
				logger.WarnCF("agent", "job checkpoint could not be written",
					map[string]interface{}{"job": j.ID, "error": perr.Error()})
			}
			done++
		},
		Evidence: func(text string) {
			_ = al.jobs.SetEvidence(j.ID, text)
		},
	})
	if err == nil {
		return nil
	}
	// Hitting an approval wall parks the job instead of retrying it: the
	// next attempt would stop at the same wall and spend the budget
	// learning nothing new. It resumes when the owner answers.
	if w, ok := tools.AsApprovalWait(err); ok {
		return tasks.WaitingPermission(w.Reason)
	}
	// A full concurrency budget is not a failure. Wait for a slot rather
	// than letting a busy runtime eventually exhaust the work's attempts.
	if cap, ok := tools.AsAtCapacity(err); ok {
		delay := cap.Delay
		if delay <= 0 {
			delay = 30 * time.Second
		}
		return tasks.Busy(delay, "waiting for a free subagent slot")
	}
	return err
}

// announceJobSettled delivers a background run's outcome to the
// conversation it was started from. It is the single place such an
// announcement happens, and the state the job landed in decides it — so a
// retry in flight can never tell the owner twice that something finished.
func (al *AgentLoop) announceJobSettled(j tasks.Job) {
	if al.bus == nil || j.Payload == nil {
		return
	}
	channel, _ := j.Payload["channel"].(string)
	if channel == "" {
		return
	}
	chatID, _ := j.Payload["chat_id"].(string)
	label, _ := j.Payload["label"].(string)

	var content string
	switch j.Status {
	case tasks.StatusSucceeded:
		content = fmt.Sprintf("Task '%s' completed.\n\nResult:\n%s", label, j.Evidence)
	case tasks.StatusWaitingPermission:
		content = fmt.Sprintf("Task '%s' is waiting for your approval before it can continue.\n\n%s", label, j.Evidence)
	default:
		// A scheduled retry is not news, and a terminal failure already
		// reached the owner through task.failed. Announcing here would be
		// the same event twice, which reads as Ghost losing the plot.
		return
	}
	al.bus.PublishInbound(bus.InboundMessage{
		Channel:  "system",
		SenderID: fmt.Sprintf("subagent:%s", j.ID),
		// Format: "original_channel:original_chat_id" for routing back,
		// the same shape the in-memory path has always used.
		ChatID:  fmt.Sprintf("%s:%s", channel, chatID),
		Content: content,
	})
}

// resumeWaitingJobs hands a session's parked background work back to the
// runner. Only permission parks are released: a job waiting for an approval
// was waiting for exactly this reply, and leaving it parked until the next
// restart would make the checkpoint the reason the work never finished.
func (al *AgentLoop) resumeWaitingJobs(sessionKey string) {
	if al.jobs == nil || sessionKey == "" {
		return
	}
	jobs, err := al.jobs.List(tasks.StatusWaitingPermission)
	if err != nil {
		logger.WarnCF("agent", "could not look for parked background work",
			map[string]interface{}{"error": err.Error()})
		return
	}
	resumed := 0
	for _, j := range jobs {
		if j.SessionKey != sessionKey {
			continue
		}
		if _, rerr := al.jobs.Resume(j.ID); rerr == nil {
			resumed++
		}
	}
	if resumed > 0 {
		logger.InfoCF("agent", "background job resumed after approval",
			map[string]interface{}{"count": resumed, "session_key": sessionKey})
		if al.jobRunner != nil {
			al.jobRunner.Wake()
		}
	}
}
