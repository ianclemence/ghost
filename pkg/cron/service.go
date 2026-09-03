package cron

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/adhocore/gronx"
	"github.com/ianclemence/ghost/pkg/bus"
)

type CronSchedule struct {
	Kind    string `json:"kind"`
	AtMS    *int64 `json:"atMs,omitempty"`
	EveryMS *int64 `json:"everyMs,omitempty"`
	Expr     string `json:"expr,omitempty"`
	Timezone string `json:"tz,omitempty"`
}

type CronPayload struct {
	Kind     string   `json:"kind"`
	Message  string   `json:"message"`
	Command  string   `json:"command,omitempty"`
	Deliver  bool     `json:"deliver"`
	Channel  string   `json:"channel,omitempty"`
	To       string   `json:"to,omitempty"`
	Target   string   `json:"target,omitempty"`
	OriginID string   `json:"origin_id,omitempty"`
	Skills   []string `json:"skills,omitempty"`
	NoAgent  bool     `json:"no_agent,omitempty"`
	Profile  string   `json:"profile,omitempty"`
}

type CronJobState struct {
	NextRunAtMS *int64 `json:"nextRunAtMs,omitempty"`
	LastRunAtMS *int64 `json:"lastRunAtMs,omitempty"`
	LastStatus  string `json:"lastStatus,omitempty"`
	LastError   string `json:"lastError,omitempty"`
}

type CronJob struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Enabled        bool         `json:"enabled"`
	LifecycleState JobState     `json:"lifecycle_state"`
	PausedAt       *time.Time   `json:"paused_at,omitempty"`
	RunCount       int          `json:"run_count"`
	LastRunAt      *time.Time   `json:"last_run_at,omitempty"`
	NextRunAt      *time.Time   `json:"next_run_at,omitempty"`
	Schedule       CronSchedule `json:"schedule"`
	Payload        CronPayload  `json:"payload"`
	State          CronJobState           `json:"state"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	CreatedAtMS    int64                  `json:"createdAtMs"`
	UpdatedAtMS    int64                  `json:"updatedAtMs"`
	DeleteAfterRun bool                   `json:"deleteAfterRun"`
	Skills         []string               `json:"skills,omitempty"`
	NoAgent        bool                   `json:"no_agent,omitempty"`
}

type CronStore struct {
	Version int       `json:"version"`
	Jobs    []CronJob `json:"jobs"`
}

type JobHandler func(job *CronJob) (string, error)

type CronService struct {
	storePath string
	store     *CronStore
	onJob     JobHandler
	bus       *bus.MessageBus
	mu        sync.RWMutex
	running   bool
	stopChan  chan struct{}
	gronx     *gronx.Gronx
}

type executionOptions struct {
	RunAsync         bool
	PreserveSchedule bool
}

func NewCronService(storePath string, onJob JobHandler, bus *bus.MessageBus) *CronService {
	cs := &CronService{
		storePath: storePath,
		onJob:     onJob,
		bus:       bus,
		gronx:     gronx.New(),
	}
	// Initialize and load store on creation
	cs.loadStore()
	return cs
}

func (cs *CronService) Start() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.running {
		return nil
	}

	if err := cs.loadStore(); err != nil {
		return fmt.Errorf("failed to load store: %w", err)
	}

	cs.recomputeNextRuns()
	if err := cs.saveStoreUnsafe(); err != nil {
		return fmt.Errorf("failed to save store: %w", err)
	}

	cs.stopChan = make(chan struct{})
	cs.running = true
	go cs.runLoop(cs.stopChan)

	return nil
}

func (cs *CronService) Stop() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if !cs.running {
		return
	}

	cs.running = false
	if cs.stopChan != nil {
		close(cs.stopChan)
		cs.stopChan = nil
	}
}

func (cs *CronService) runLoop(stopChan chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			cs.checkJobs()
		}
	}
}

func (cs *CronService) checkJobs() {
	cs.mu.Lock()

	if !cs.running {
		cs.mu.Unlock()
		return
	}

	now := time.Now().UnixMilli()
	var dueJobIDs []string

	// Collect jobs that are due (we need to copy them to execute outside lock)
	for i := range cs.store.Jobs {
		job := &cs.store.Jobs[i]
		if job.Enabled && job.LifecycleState != JobStatePaused && job.State.NextRunAtMS != nil && *job.State.NextRunAtMS <= now {
			dueJobIDs = append(dueJobIDs, job.ID)
		}
	}

	// Reset next run for due jobs before unlocking to avoid duplicate execution.
	dueMap := make(map[string]bool, len(dueJobIDs))
	for _, jobID := range dueJobIDs {
		dueMap[jobID] = true
	}
	for i := range cs.store.Jobs {
		if dueMap[cs.store.Jobs[i].ID] {
			cs.store.Jobs[i].State.NextRunAtMS = nil
			cs.store.Jobs[i].NextRunAt = nil
		}
	}

	if err := cs.saveStoreUnsafe(); err != nil {
		log.Printf("[cron] failed to save store: %v", err)
	}

	cs.mu.Unlock()

	// Execute jobs outside lock.
	for _, jobID := range dueJobIDs {
		cs.notifyUpdate(jobID, "started")
		cs.executeJobByID(jobID, executionOptions{})
	}
}

func (cs *CronService) notifyUpdate(jobID string, status string) {
	if cs.bus == nil {
		return
	}

	cs.mu.RLock()
	jobCopy, _ := cs.findJobUnsafe(jobID)
	cs.mu.RUnlock()

	if jobCopy == nil {
		return
	}

	cs.publishUpdate(jobCopy, status)
}

func (cs *CronService) notifyUpdateUnsafe(jobID string, status string) {
	if cs.bus == nil {
		return
	}

	job, _ := cs.findJobUnsafe(jobID)
	if job == nil {
		return
	}

	cs.publishUpdate(job, status)
}

func (cs *CronService) publishUpdate(job *CronJob, status string) {
	// Send a copy to avoid data race if the job object is modified later
	jobCopy := *job

	cs.bus.PublishOutbound(bus.OutboundMessage{
		Channel: "system",
		ChatID:  "cron_update",
		Content: status,
		Metadata: map[string]interface{}{
			"type":   "cron_update",
			"job_id": job.ID,
			"status": status,
			"job":    &jobCopy,
		},
	})
}

func (cs *CronService) executeJobByID(jobID string, opts executionOptions) {
	startTime := time.Now().UnixMilli()

	cs.mu.RLock()
	var callbackJob *CronJob
	for i := range cs.store.Jobs {
		job := &cs.store.Jobs[i]
		if job.ID == jobID {
			jobCopy := *job
			callbackJob = &jobCopy
			break
		}
	}
	cs.mu.RUnlock()

	if callbackJob == nil {
		return
	}

	var err error
	if cs.onJob != nil {
		_, err = cs.onJob(callbackJob)
	}

	// Now acquire lock to update state
	cs.mu.Lock()
	defer cs.mu.Unlock()

	var job *CronJob
	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID == jobID {
			job = &cs.store.Jobs[i]
			break
		}
	}
	if job == nil {
		log.Printf("[cron] job %s disappeared before state update", jobID)
		return
	}

	job.State.LastRunAtMS = &startTime
	lastRunAt := time.UnixMilli(startTime)
	job.LastRunAt = &lastRunAt
	job.RunCount++
	job.LifecycleState = JobStateRunning
	job.UpdatedAtMS = time.Now().UnixMilli()

	if err != nil {
		job.State.LastStatus = "error"
		job.State.LastError = err.Error()
	} else {
		job.State.LastStatus = "ok"
		job.State.LastError = ""
	}

	// Compute next run time unless this is a manual async trigger that preserves cadence
	if !opts.PreserveSchedule {
		if job.Schedule.Kind == "at" {
			if job.DeleteAfterRun {
				cs.removeJobUnsafe(job.ID)
			} else {
				job.Enabled = false
				job.State.NextRunAtMS = nil
				job.NextRunAt = nil
				job.LifecycleState = JobStatePaused
			}
		} else {
			nextRun := cs.computeNextRun(&job.Schedule, time.Now().UnixMilli())
			job.State.NextRunAtMS = nextRun
			if nextRun != nil {
				next := time.UnixMilli(*nextRun)
				job.NextRunAt = &next
			} else {
				job.NextRunAt = nil
			}
			job.LifecycleState = JobStateActive
		}
	} else {
		if job.Enabled {
			job.LifecycleState = JobStateActive
		} else {
			job.LifecycleState = JobStatePaused
		}
	}

	if err := cs.saveStoreUnsafe(); err != nil {
		log.Printf("[cron] failed to save store: %v", err)
	}

	cs.notifyUpdateUnsafe(jobID, "finished")
}

func (cs *CronService) computeNextRun(schedule *CronSchedule, nowMS int64) *int64 {
	if schedule.Kind == "at" {
		if schedule.AtMS != nil && *schedule.AtMS > nowMS {
			return schedule.AtMS
		}
		return nil
	}

	if schedule.Kind == "every" {
		if schedule.EveryMS == nil || *schedule.EveryMS <= 0 {
			return nil
		}
		next := nowMS + *schedule.EveryMS
		return &next
	}

	if schedule.Kind == "cron" {
		if schedule.Expr == "" {
			return nil
		}

		// Use gronx to calculate next run time
		now := time.UnixMilli(nowMS)
		nextTime, err := gronx.NextTickAfter(schedule.Expr, now, false)
		if err != nil {
			log.Printf("[cron] failed to compute next run for expr '%s': %v", schedule.Expr, err)
			return nil
		}

		nextMS := nextTime.UnixMilli()
		return &nextMS
	}

	return nil
}

func (cs *CronService) recomputeNextRuns() {
	now := time.Now().UnixMilli()
	for i := range cs.store.Jobs {
		job := &cs.store.Jobs[i]
		if job.LifecycleState == "" {
			job.LifecycleState = JobStateActive
		}
		if job.Enabled && job.LifecycleState != JobStatePaused {
			job.State.NextRunAtMS = cs.computeNextRun(&job.Schedule, now)
			if job.State.NextRunAtMS != nil {
				next := time.UnixMilli(*job.State.NextRunAtMS)
				job.NextRunAt = &next
			} else {
				job.NextRunAt = nil
			}
		} else {
			job.State.NextRunAtMS = nil
			job.NextRunAt = nil
		}
	}
}

func (cs *CronService) getNextWakeMS() *int64 {
	var nextWake *int64
	for _, job := range cs.store.Jobs {
		if job.Enabled && job.LifecycleState != JobStatePaused && job.State.NextRunAtMS != nil {
			if nextWake == nil || *job.State.NextRunAtMS < *nextWake {
				nextWake = job.State.NextRunAtMS
			}
		}
	}
	return nextWake
}

func (cs *CronService) Load() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.loadStore()
}

func (cs *CronService) SetOnJob(handler JobHandler) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.onJob = handler
}

func (cs *CronService) loadStore() error {
	cs.store = &CronStore{
		Version: 1,
		Jobs:    []CronJob{},
	}

	data, err := os.ReadFile(cs.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(string(data)) == "" {
		return cs.saveStoreUnsafe()
	}

	if err := json.Unmarshal(data, cs.store); err != nil {
		if strings.Contains(err.Error(), "unexpected end of JSON input") {
			backup := cs.storePath + ".corrupt." + fmt.Sprintf("%d", time.Now().Unix())
			_ = os.WriteFile(backup, data, 0644)
			return cs.saveStoreUnsafe()
		}
		return err
	}

	for i := range cs.store.Jobs {
		job := &cs.store.Jobs[i]
		if job.LifecycleState == "" {
			if job.Enabled {
				job.LifecycleState = JobStateActive
			} else {
				job.LifecycleState = JobStatePaused
			}
		}
		if job.State.LastRunAtMS != nil {
			ts := time.UnixMilli(*job.State.LastRunAtMS)
			job.LastRunAt = &ts
		}
		if job.State.NextRunAtMS != nil && job.Enabled && job.LifecycleState != JobStatePaused {
			ts := time.UnixMilli(*job.State.NextRunAtMS)
			job.NextRunAt = &ts
		}
	}

	return nil
}

func (cs *CronService) saveStoreUnsafe() error {
	dir := filepath.Dir(cs.storePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cs.store, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cs.storePath, data, 0644)
}

func (cs *CronService) AddJob(name string, schedule CronSchedule, message string, deliver bool, channel, to string, metadata map[string]interface{}) (*CronJob, error) {
	return cs.AddJobWithOptions(name, schedule, message, deliver, channel, to, metadata, nil, false, "")
}

func (cs *CronService) AddJobWithOptions(name string, schedule CronSchedule, message string, deliver bool, channel, to string, metadata map[string]interface{}, skills []string, noAgent bool, profile string) (*CronJob, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	now := time.Now().UnixMilli()

	deleteAfterRun := (schedule.Kind == "at")

	job := CronJob{
		ID:             generateID(),
		Name:           name,
		Enabled:        true,
		LifecycleState: JobStateActive,
		Schedule:       schedule,
		Payload: CronPayload{
			Kind:    "agent_turn",
			Message: message,
			Deliver: deliver,
			Channel: channel,
			To:      to,
			Target:  "origin",
			Skills:  skills,
			NoAgent: noAgent,
			Profile: profile,
		},
		State: CronJobState{
			NextRunAtMS: cs.computeNextRun(&schedule, now),
		},
		Metadata:       metadata,
		CreatedAtMS:    now,
		UpdatedAtMS:    now,
		DeleteAfterRun: deleteAfterRun,
		Skills:         skills,
		NoAgent:        noAgent,
	}
	if job.State.NextRunAtMS != nil {
		next := time.UnixMilli(*job.State.NextRunAtMS)
		job.NextRunAt = &next
	}

	cs.store.Jobs = append(cs.store.Jobs, job)
	if err := cs.saveStoreUnsafe(); err != nil {
		return nil, err
	}

	cs.notifyUpdateUnsafe(job.ID, "added")

	return &job, nil
}

func (cs *CronService) SaveJob(job *CronJob) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID == job.ID {
			cs.store.Jobs[i] = *job
			cs.store.Jobs[i].UpdatedAtMS = time.Now().UnixMilli()
			return cs.saveStoreUnsafe()
		}
	}
	return fmt.Errorf("job not found")
}

func (cs *CronService) PauseJob(id string) error {
	job := cs.EnableJob(id, false)
	if job == nil {
		return fmt.Errorf("job not found")
	}
	return nil
}

func (cs *CronService) ResumeJob(id string) error {
	job := cs.EnableJob(id, true)
	if job == nil {
		return fmt.Errorf("job not found")
	}
	return nil
}

func (cs *CronService) RunJobNow(id string) error {
	cs.mu.RLock()
	_, idx := cs.findJobUnsafe(id)
	cs.mu.RUnlock()
	if idx < 0 {
		return fmt.Errorf("job not found")
	}
	go cs.executeJobByID(id, executionOptions{RunAsync: true, PreserveSchedule: true})
	return nil
}

func (cs *CronService) UpdateJob(id string, updates JobUpdate) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	job, idx := cs.findJobUnsafe(id)
	if idx < 0 {
		return fmt.Errorf("job not found")
	}

	if updates.Name != nil {
		job.Name = *updates.Name
	}
	if updates.Schedule != nil {
		job.Schedule = *updates.Schedule
	}
	if updates.Message != nil {
		job.Payload.Message = *updates.Message
	}
	if updates.Command != nil {
		job.Payload.Command = *updates.Command
	}
	if updates.Deliver != nil {
		job.Payload.Deliver = *updates.Deliver
	}
	if updates.Channel != nil {
		job.Payload.Channel = *updates.Channel
	}
	if updates.To != nil {
		job.Payload.To = *updates.To
	}
	if updates.Target != nil {
		job.Payload.Target = *updates.Target
	}
	if updates.Enabled != nil {
		job.Enabled = *updates.Enabled
		if job.Enabled {
			job.LifecycleState = JobStateActive
			job.PausedAt = nil
		} else {
			job.LifecycleState = JobStatePaused
			nowTime := time.Now()
			job.PausedAt = &nowTime
		}
	}
	if updates.Skills != nil {
		job.Skills = updates.Skills
		job.Payload.Skills = updates.Skills
	}
	if updates.NoAgent != nil {
		job.NoAgent = *updates.NoAgent
		job.Payload.NoAgent = *updates.NoAgent
	}

	nowMS := time.Now().UnixMilli()
	if job.Enabled && job.LifecycleState != JobStatePaused {
		job.State.NextRunAtMS = cs.computeNextRun(&job.Schedule, nowMS)
		if job.State.NextRunAtMS != nil {
			next := time.UnixMilli(*job.State.NextRunAtMS)
			job.NextRunAt = &next
		} else {
			job.NextRunAt = nil
		}
	} else {
		job.State.NextRunAtMS = nil
		job.NextRunAt = nil
	}
	job.UpdatedAtMS = nowMS

	cs.store.Jobs[idx] = *job
	return cs.saveStoreUnsafe()
}

func (cs *CronService) GetJobStatus(id string) (JobStatus, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	job, idx := cs.findJobUnsafe(id)
	if idx < 0 {
		return JobStatus{}, fmt.Errorf("job not found")
	}
	return JobStatus{
		ID:        job.ID,
		Name:      job.Name,
		State:     job.LifecycleState,
		Enabled:   job.Enabled,
		RunCount:  job.RunCount,
		LastRunAt: job.LastRunAt,
		NextRunAt: job.NextRunAt,
		LastError: job.State.LastError,
	}, nil
}

func (cs *CronService) GetJob(id string) (*CronJob, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	job, idx := cs.findJobUnsafe(id)
	if idx < 0 {
		return nil, false
	}
	copy := *job
	return &copy, true
}

func (cs *CronService) findJobUnsafe(id string) (*CronJob, int) {
	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID == id {
			return &cs.store.Jobs[i], i
		}
	}
	return nil, -1
}

func (cs *CronService) RemoveJob(jobID string) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	return cs.removeJobUnsafe(jobID)
}

func (cs *CronService) removeJobUnsafe(jobID string) bool {
	before := len(cs.store.Jobs)
	var jobs []CronJob
	for _, job := range cs.store.Jobs {
		if job.ID != jobID {
			jobs = append(jobs, job)
		}
	}
	cs.store.Jobs = jobs
	removed := len(cs.store.Jobs) < before

	if removed {
		if err := cs.saveStoreUnsafe(); err != nil {
			log.Printf("[cron] failed to save store after remove: %v", err)
		}
		cs.notifyUpdateUnsafe(jobID, "removed")
	}

	return removed
}

func (cs *CronService) EnableJob(jobID string, enabled bool) *CronJob {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for i := range cs.store.Jobs {
		job := &cs.store.Jobs[i]
		if job.ID == jobID {
			job.Enabled = enabled
			if enabled && job.LifecycleState == JobStatePaused {
				job.LifecycleState = JobStateActive
				job.PausedAt = nil
			}
			if !enabled {
				job.LifecycleState = JobStatePaused
				nowTime := time.Now()
				job.PausedAt = &nowTime
			}
			job.UpdatedAtMS = time.Now().UnixMilli()

			if enabled {
				job.State.NextRunAtMS = cs.computeNextRun(&job.Schedule, time.Now().UnixMilli())
				if job.State.NextRunAtMS != nil {
					next := time.UnixMilli(*job.State.NextRunAtMS)
					job.NextRunAt = &next
				}
			} else {
				job.State.NextRunAtMS = nil
				job.NextRunAt = nil
			}

			if err := cs.saveStoreUnsafe(); err != nil {
				log.Printf("[cron] failed to save store after enable: %v", err)
			}
			cs.notifyUpdateUnsafe(job.ID, "updated")
			return job
		}
	}

	return nil
}

func (cs *CronService) ListJobs(includeDisabled bool) []CronJob {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if includeDisabled {
		return cs.store.Jobs
	}

	var enabled []CronJob
	for _, job := range cs.store.Jobs {
		if job.Enabled {
			enabled = append(enabled, job)
		}
	}

	return enabled
}

func (cs *CronService) ListJobsByProfile(profile string, includeDisabled bool) []CronJob {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	var result []CronJob
	for _, job := range cs.store.Jobs {
		if job.Payload.Profile != profile {
			continue
		}
		if includeDisabled || job.Enabled {
			result = append(result, job)
		}
	}
	return result
}

func (cs *CronService) PauseJobsByProfile(profile string) int {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	count := 0
	for i := range cs.store.Jobs {
		job := &cs.store.Jobs[i]
		if job.Payload.Profile == profile && job.Enabled {
			job.Enabled = false
			job.LifecycleState = JobStatePaused
			job.State.NextRunAtMS = nil
			job.NextRunAt = nil
			job.UpdatedAtMS = time.Now().UnixMilli()
			count++
		}
	}
	if count > 0 {
		cs.saveStoreUnsafe()
	}
	return count
}

func (cs *CronService) ResumeJobsByProfile(profile string) int {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	nowMS := time.Now().UnixMilli()
	count := 0
	for i := range cs.store.Jobs {
		job := &cs.store.Jobs[i]
		if job.Payload.Profile == profile && !job.Enabled && job.LifecycleState == JobStatePaused {
			job.Enabled = true
			job.LifecycleState = JobStateActive
			job.PausedAt = nil
			job.State.NextRunAtMS = cs.computeNextRun(&job.Schedule, nowMS)
			if job.State.NextRunAtMS != nil {
				next := time.UnixMilli(*job.State.NextRunAtMS)
				job.NextRunAt = &next
			}
			job.UpdatedAtMS = nowMS
			count++
		}
	}
	if count > 0 {
		cs.saveStoreUnsafe()
	}
	return count
}

func (cs *CronService) RemoveJobsByProfile(profile string) int {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	before := len(cs.store.Jobs)
	var kept []CronJob
	for _, job := range cs.store.Jobs {
		if job.Payload.Profile != profile {
			kept = append(kept, job)
		}
	}
	removed := before - len(kept)
	cs.store.Jobs = kept
	if removed > 0 {
		cs.saveStoreUnsafe()
	}
	return removed
}

func (cs *CronService) Status() map[string]interface{} {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	var enabledCount int
	for _, job := range cs.store.Jobs {
		if job.Enabled {
			enabledCount++
		}
	}

	return map[string]interface{}{
		"enabled":      cs.running,
		"jobs":         len(cs.store.Jobs),
		"nextWakeAtMS": cs.getNextWakeMS(),
	}
}

func generateID() string {
	// Use crypto/rand for better uniqueness under concurrent access
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to time-based if crypto/rand fails
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
