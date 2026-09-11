package commands

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/scheduled"
)

var intervalRegex = regexp.MustCompile(`^(\d+h)?(\d+m)?(\d+s)?$`)

const (
	loopDefaultInterval = 5 * time.Minute
	loopMinInterval     = 30 * time.Second
	loopMaxInterval     = 15 * time.Minute
	loopFloorInterval   = 5 * time.Minute
)

func loopHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.Scheduler == nil {
		return req.Reply("Scheduling is unavailable.")
	}
	svc := rt.Scheduler()
	if svc == nil {
		return req.Reply("Scheduling is unavailable.")
	}

	text := strings.TrimSpace(strings.TrimPrefix(req.Text, "/loop"))
	if text == "" {
		return req.Reply("Usage: /loop [interval] <prompt>\nExamples:\n- `/loop 5m check for new emails`\n- `/loop monitor the server` (self-paced, every 5m)")
	}

	interval, prompt := parseLoopArgs(text)
	if prompt == "" {
		return req.Reply("Usage: /loop [interval] <prompt>")
	}

	seconds := int(interval.Seconds())
	if seconds < int(loopMinInterval.Seconds()) {
		return req.Reply(fmt.Sprintf("Minimum interval is %v.", loopMinInterval))
	}

	mode := "fixed"
	if len(strings.Fields(text)) == len(strings.Fields(prompt)) {
		mode = "self_paced"
	}

	now := time.Now().UTC()
	next := now.Add(interval)
	item := &scheduled.ScheduledItem{
		Type:         scheduled.TypeAutomation,
		Title:        prompt,
		Description:  prompt,
		State:        scheduled.StateScheduled,
		Timezone:     "UTC",
		Schedule:     scheduled.Schedule{Kind: scheduled.ScheduleEvery, Every: interval},
		Action:       scheduled.Action{Kind: scheduled.ActionAgentTurn, Content: prompt, Deliver: true},
		Channel:      req.Channel,
		ChatID:       req.ChatID,
		DeliveryMode: scheduled.DeliveryOrigin,
		Source:       "loop",
		CreatedBy:    "agent",
		NextRunAt:    &next,
		MaxRetries:   3,
	}
	if err := svc.CreateItem(item); err != nil {
		return req.Reply(fmt.Sprintf("Failed to create loop: %v", err))
	}

	return req.Reply(fmt.Sprintf("Loop created: ID=%s, interval=%v, mode=%s\nPrompt: %s",
		item.ID, interval, mode, prompt))
}

func parseLoopArgs(text string) (time.Duration, string) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return loopDefaultInterval, ""
	}

	first := fields[0]
	if d, err := time.ParseDuration(first); err == nil {
		if d >= loopMinInterval {
			prompt := strings.Join(fields[1:], " ")
			return d, prompt
		}
	}

	if match := intervalRegex.FindStringSubmatch(first); match != nil {
		var total int64
		if match[1] != "" {
			h, _ := strconv.ParseInt(match[1][:len(match[1])-1], 10, 64)
			total += h * 3600
		}
		if match[2] != "" {
			m, _ := strconv.ParseInt(match[2][:len(match[2])-1], 10, 64)
			total += m * 60
		}
		if match[3] != "" {
			s, _ := strconv.ParseInt(match[3][:len(match[3])-1], 10, 64)
			total += s
		}
		if total >= int64(loopMinInterval.Seconds()) {
			prompt := strings.Join(fields[1:], " ")
			return time.Duration(total) * time.Second, prompt
		}
	}

	return loopDefaultInterval, text
}

func computeDigest(response string) string {
	normalized := strings.TrimSpace(response)
	normalized = regexp.MustCompile(`\s+`).ReplaceAllString(normalized, " ")
	h := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", h[:8])
}

func loopsHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.Scheduler == nil {
		return req.Reply("Scheduling is unavailable.")
	}
	svc := rt.Scheduler()
	if svc == nil {
		return req.Reply("Scheduling is unavailable.")
	}
	items, err := svc.ListItems("", "", 200)
	if err != nil {
		return req.Reply(fmt.Sprintf("Failed to list loops: %v", err))
	}
	var sb strings.Builder
	sb.WriteString("### Active loops\n\n")
	count := 0
	for _, it := range items {
		if it == nil || it.Source != "loop" {
			continue
		}
		count++
		next := "—"
		if it.NextRunAt != nil {
			next = it.NextRunAt.Format("2006-01-02 15:04")
		}
		sb.WriteString(fmt.Sprintf("- `%s` every %s — %s (next %s)\n", it.ID, it.Schedule.Every, it.Title, next))
	}
	if count == 0 {
		return req.Reply("No active loops.")
	}
	return req.Reply(sb.String())
}

func stoploopHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.Scheduler == nil {
		return req.Reply("Scheduling is unavailable.")
	}
	svc := rt.Scheduler()
	if svc == nil {
		return req.Reply("Scheduling is unavailable.")
	}
	args := strings.Fields(req.Text)
	if len(args) < 2 {
		return req.Reply("Usage: /stoploop <job_id>")
	}
	jobID := args[1]
	if err := svc.CancelItem(jobID); err != nil {
		return req.Reply(fmt.Sprintf("Failed to stop loop: %v", err))
	}
	return req.Reply(fmt.Sprintf("Loop %s stopped.", jobID))
}
