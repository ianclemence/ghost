package commands

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var intervalRegex = regexp.MustCompile(`^(\d+h)?(\d+m)?(\d+s)?$`)

const (
	loopDefaultInterval = 5 * time.Minute
	loopMinInterval     = 30 * time.Second
	loopMaxInterval     = 15 * time.Minute
	loopFloorInterval   = 5 * time.Minute
)

func loopHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.Tools == nil {
		return req.Reply("Loop tool unavailable.")
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

	tool, ok := rt.Tools.Get("cron")
	if !ok {
		return req.Reply("Cron tool not available.")
	}
	if ct, ok := tool.(interface {
		SetContext(channel, chatID string)
	}); ok {
		ct.SetContext(req.Channel, req.ChatID)
	}

	mode := "fixed"
	if len(strings.Fields(text)) == len(strings.Fields(prompt)) {
		mode = "self_paced"
	}

	res := tool.Execute(ctx, map[string]interface{}{
		"action":        "add",
		"message":       prompt,
		"every_seconds": float64(seconds),
		"deliver":       true,
	})

	if res.IsError {
		return req.Reply(fmt.Sprintf("Failed to create loop: %s", res.ForLLM))
	}

	var jobID string
	if strings.Contains(res.ForLLM, "id:") {
		parts := strings.Split(res.ForLLM, "id:")
		if len(parts) > 1 {
			jobID = strings.TrimSpace(strings.Split(parts[1], "\n")[0])
		}
	}

	return req.Reply(fmt.Sprintf("Loop created: ID=%s, interval=%v, mode=%s\nPrompt: %s",
		jobID, interval, mode, prompt))
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
	if rt == nil || rt.Tools == nil {
		return req.Reply("Cron tool unavailable.")
	}

	tool, ok := rt.Tools.Get("cron")
	if !ok {
		return req.Reply("Cron tool not available.")
	}

	res := tool.Execute(ctx, map[string]interface{}{
		"action": "list",
	})

	if res.IsError {
		return req.Reply(fmt.Sprintf("Failed to list loops: %s", res.ForLLM))
	}

	return req.Reply(res.ForLLM)
}

func stoploopHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.Tools == nil {
		return req.Reply("Cron tool unavailable.")
	}

	args := strings.Fields(req.Text)
	if len(args) < 2 {
		return req.Reply("Usage: /stoploop <job_id>")
	}

	jobID := args[1]

	tool, ok := rt.Tools.Get("cron")
	if !ok {
		return req.Reply("Cron tool not available.")
	}

	res := tool.Execute(ctx, map[string]interface{}{
		"action":  "disable",
		"job_id":  jobID,
	})

	if res.IsError {
		return req.Reply(fmt.Sprintf("Failed to stop loop: %s", res.ForLLM))
	}

	return req.Reply(fmt.Sprintf("Loop %s stopped.", jobID))
}
