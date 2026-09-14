package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/proactive"
)

// Deferred delivery: notices that cannot be delivered now (no live session,
// quiet hours holding a non-urgent notice, offline) are enqueued to a
// bounded on-disk outbox instead of being dropped. PollProactive drains
// due items first. This is the offline-first half of the hardware story:
// a button press at 3am with no session open still reaches the owner.

type heldNotice struct {
	Notice    Notice    `json:"notice"`
	NotBefore time.Time `json:"not_before"`
	Enqueued  time.Time `json:"enqueued_at"`
}

const (
	outboxMaxItems = 50
	outboxFile     = "outbox.jsonl"
)

func outboxPath(workspace string) string {
	return filepath.Join(workspace, "proactive", outboxFile)
}

// enqueueHeld stores a notice for later delivery. Bounded: oldest dropped
// past the cap. Best-effort: storage failure means the notice is lost,
// same as the previous drop behavior — never an error path.
func enqueueHeld(workspace string, nt Notice, notBefore time.Time) {
	if workspace == "" {
		return
	}
	if notBefore.IsZero() {
		notBefore = time.Now().UTC()
	}
	path := outboxPath(workspace)
	var items []heldNotice
	if raw, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var h heldNotice
			if err := json.Unmarshal([]byte(line), &h); err == nil && h.Notice.Message != "" {
				items = append(items, h)
			}
		}
	}
	items = append(items, heldNotice{Notice: nt, NotBefore: notBefore.UTC(), Enqueued: time.Now().UTC()})
	for len(items) > outboxMaxItems {
		items = items[1:]
	}
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	var b strings.Builder
	for _, h := range items {
		raw, err := json.Marshal(h)
		if err != nil {
			continue
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	_ = os.WriteFile(path, []byte(b.String()), 0600)
}

// dueHeld returns notices whose NotBefore passed, removing them from disk.
// Delivery itself still gates through MaybeNotify (budget/cooldown/dedupe).
func dueHeld(workspace string, now time.Time) []Notice {
	if workspace == "" {
		return nil
	}
	path := outboxPath(workspace)
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	var due []Notice
	var keep []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var h heldNotice
		if err := json.Unmarshal([]byte(line), &h); err != nil || h.Notice.Message == "" {
			continue
		}
		if !now.Before(h.NotBefore) {
			due = append(due, h.Notice)
		} else {
			keep = append(keep, line)
		}
	}
	_ = f.Close()
	if len(due) == 0 {
		return nil
	}
	_ = os.WriteFile(path, []byte(strings.Join(keep, "\n")+trailingNewline(len(keep))), 0600)
	return due
}

func trailingNewline(n int) string {
	if n == 0 {
		return ""
	}
	return "\n"
}

// quietResumeTime returns when quiet hours end in the user timezone (the
// moment a held non-urgent notice becomes deliverable).
func quietResumeTime(al *AgentLoop, now time.Time) time.Time {
	pol := proactive.Load(al.workspace)
	loc := proactive.UserLocation(al.pcStore)
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	end := midnight.Add(time.Duration(pol.QuietEnd) * time.Minute)
	if !end.After(local) {
		end = end.Add(24 * time.Hour)
	}
	return end.UTC()
}
