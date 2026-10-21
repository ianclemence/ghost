package main

import (
	"context"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/tools"
	"github.com/ianclemence/ghost/pkg/updaterun"
)

// How long Ghost waits after starting before it checks itself: long enough
// for the services it depends on to settle, short enough that the owner who
// asked for the update is still looking at the conversation.
const updateReportDelay = 20 * time.Second

// An update asked for longer ago than this has no one waiting on its answer.
const updateReportMaxAge = 2 * time.Hour

// updateOutcomeText says how an update went, in the owner's terms: whether it
// took, and if Ghost is healthy now. It never claims more than the check showed.
func updateOutcomeText(from, now string, rows []tools.SelfCheckRow) string {
	verdict := tools.SelfCheckVerdict(rows)
	var head string
	switch {
	case from == now:
		head = "The update didn't take. I'm still on " + now + ": the new release didn't start properly, so the previous one was put back."
	default:
		head = "I'm back, now on " + now + " (it was " + from + ")."
	}
	switch {
	case len(rows) == 0:
		return head + " I couldn't run my self-check just now, so I can't say how I'm doing."
	case verdict == "healthy":
		return head + " I checked myself and everything is working."
	default:
		return head + " I checked myself and it " + verdict + ":\n" + tools.FormatSelfCheck(now, rows, true)
	}
}

// reportUpdateOutcome runs once at startup. If an update was asked for from a
// conversation, it checks Ghost and tells that conversation how it went.
func reportUpdateOutcome(al *agent.AgentLoop) {
	p, ok := updaterun.TakePending(updateReportMaxAge)
	if !ok {
		return
	}
	time.Sleep(updateReportDelay)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	now := tools.RunningVersion()
	rows := al.SelfCheckRows(ctx)
	text := updateOutcomeText(p.From, now, rows)
	logger.InfoCF("update", "reporting the update outcome", map[string]interface{}{"from": p.From, "now": now, "verdict": tools.SelfCheckVerdict(rows)})
	if p.Channel == "" {
		return
	}
	al.DeliverToOwner(p.Channel, p.ChatID, text, map[string]interface{}{
		"announce": "update",
		"urgent":   p.From == now || tools.SelfCheckVerdict(rows) == "problems",
	})
}
