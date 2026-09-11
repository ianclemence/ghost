package scheduled

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"time"
)

// Legacy cron/jobs.json migration.
//
// Ghost previously ran a second scheduler (pkg/cron) backed by a JSON file.
// The authoritative scheduler is now this package. This reader migrates any
// legacy jobs into scheduled_items exactly once and idempotently.
//
// The legacy JSON schema is declared here (rather than importing the retired
// pkg/cron) so the migration cannot keep that package alive.

type legacyCronStore struct {
	Version int             `json:"version"`
	Jobs    []legacyCronJob `json:"jobs"`
}

type legacyCronSchedule struct {
	Kind     string `json:"kind"`
	AtMS     *int64 `json:"atMs,omitempty"`
	EveryMS  *int64 `json:"everyMs,omitempty"`
	Expr     string `json:"expr,omitempty"`
	Timezone string `json:"tz,omitempty"`
}

type legacyCronPayload struct {
	Message string `json:"message"`
	Command string `json:"command,omitempty"`
	Deliver bool   `json:"deliver"`
	Channel string `json:"channel,omitempty"`
	To      string `json:"to,omitempty"`
	Target  string `json:"target,omitempty"`
}

type legacyCronJob struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Enabled        bool               `json:"enabled"`
	LifecycleState string             `json:"lifecycle_state"`
	Schedule       legacyCronSchedule `json:"schedule"`
	Payload        legacyCronPayload  `json:"payload"`
}

// LegacyCronMigrationResult reports what a migration pass did.
type LegacyCronMigrationResult struct {
	Migrated int
	Skipped  int
	Errors   []string
}

// MigrateLegacyCron reads a legacy cron/jobs.json file and migrates each job
// into the authoritative store. It is safe to run repeatedly: item IDs are
// deterministic functions of the job's normalized schedule + content, so a
// second run produces no duplicates. A missing or empty file is a no-op. A
// malformed file returns an error without destroying anything.
func MigrateLegacyCron(legacyPath string, store *Store) (LegacyCronMigrationResult, error) {
	var res LegacyCronMigrationResult
	if store == nil {
		return res, fmt.Errorf("nil scheduled store")
	}
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return res, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return res, nil
	}
	var legacy legacyCronStore
	if err := json.Unmarshal(data, &legacy); err != nil {
		return res, fmt.Errorf("legacy cron store is malformed: %w", err)
	}
	for _, job := range legacy.Jobs {
		item, err := legacyJobToItem(job)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", job.ID, err))
			continue
		}
		if existing, _ := store.Get(item.ID); existing != nil {
			res.Skipped++
			continue
		}
		if err := store.Create(item); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", job.ID, err))
			continue
		}
		res.Migrated++
	}
	return res, nil
}

func legacyJobToItem(job legacyCronJob) (*ScheduledItem, error) {
	content := strings.TrimSpace(job.Payload.Message)
	command := strings.TrimSpace(job.Payload.Command)
	if content == "" && command == "" {
		return nil, fmt.Errorf("job has no message or command")
	}
	sched, recurring, err := legacySchedule(job.Schedule)
	if err != nil {
		return nil, err
	}
	item := &ScheduledItem{
		ID:          legacyItemID(job),
		Title:       firstNonEmpty(job.Name, content, command),
		Description: content,
		Schedule:    sched,
		Timezone:    firstNonEmpty(job.Schedule.Timezone, "UTC"),
		Channel:     job.Payload.Channel,
		ChatID:      job.Payload.To,
		Source:      "migration",
		CreatedBy:   "legacy-cron",
		MaxRetries:  3,
		// Paused legacy jobs migrate paused; active ones migrate scheduled.
		State: StateScheduled,
	}
	if job.LifecycleState == "paused" || !job.Enabled {
		item.State = StatePaused
	}
	if command != "" {
		item.Type = TypeAutomation
		item.Action = Action{Kind: ActionCommand, Command: command}
	} else {
		item.Action = Action{Kind: ActionAgentTurn, Content: content, Deliver: job.Payload.Deliver}
		if recurring {
			item.Type = TypeAutomation
		} else {
			item.Type = TypeReminder
		}
	}
	if item.Channel == "" {
		item.Channel = job.Payload.Channel
	}
	return item, nil
}

func legacySchedule(s legacyCronSchedule) (Schedule, bool, error) {
	switch strings.ToLower(strings.TrimSpace(s.Kind)) {
	case "at":
		if s.AtMS == nil {
			return Schedule{}, false, fmt.Errorf("at schedule missing atMs")
		}
		t := time.UnixMilli(*s.AtMS).UTC()
		return Schedule{Kind: ScheduleAt, At: &t}, false, nil
	case "every":
		if s.EveryMS == nil || *s.EveryMS <= 0 {
			return Schedule{}, false, fmt.Errorf("every schedule missing everyMs")
		}
		return Schedule{Kind: ScheduleEvery, Every: time.Duration(*s.EveryMS) * time.Millisecond}, true, nil
	case "cron":
		if strings.TrimSpace(s.Expr) == "" {
			return Schedule{}, false, fmt.Errorf("cron schedule missing expr")
		}
		return Schedule{Kind: ScheduleCron, Expr: strings.TrimSpace(s.Expr)}, true, nil
	default:
		return Schedule{}, false, fmt.Errorf("unknown schedule kind %q", s.Kind)
	}
}

// legacyItemID derives a stable, deterministic ID from the job's normalized
// schedule + content. Row IDs that change across runs are deliberately not
// used. A same-content/same-schedule job is the same item.
func legacyItemID(job legacyCronJob) string {
	key := strings.ToLower(strings.Join([]string{
		strings.TrimSpace(job.Schedule.Kind),
		strings.TrimSpace(job.Schedule.Expr),
		msString(job.Schedule.AtMS),
		msString(job.Schedule.EveryMS),
		strings.TrimSpace(job.Payload.Message),
		strings.TrimSpace(job.Payload.Command),
		strings.TrimSpace(job.Payload.Channel),
		strings.TrimSpace(job.Payload.To),
	}, "|"))
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return fmt.Sprintf("legacy-cron-%016x", h.Sum64())
}

func msString(v *int64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%d", *v)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
