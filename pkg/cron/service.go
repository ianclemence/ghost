package cron

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/robfig/cron/v3"
)

type CronSchedule struct {
	Kind    string `json:"kind"` // "at", "every", "cron"
	AtMS    *int64 `json:"at_ms,omitempty"`
	EveryMS *int64 `json:"every_ms,omitempty"`
	Expr    string `json:"expr,omitempty"`
}

type JobPayload struct {
	Command string `json:"command,omitempty"`
	Channel string `json:"channel,omitempty"`
	To      string `json:"to,omitempty"` // ChatID
	Deliver bool   `json:"deliver"`
	Message string `json:"message,omitempty"`
}

type JobState struct {
	LastRunAtMS *int64 `json:"last_run_at_ms,omitempty"`
	NextRunAtMS *int64 `json:"next_run_at_ms,omitempty"`
	Error       string `json:"error,omitempty"`
	RunCount    int64  `json:"run_count"`
}

type CronJob struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Schedule  CronSchedule `json:"schedule"`
	Payload   JobPayload   `json:"payload"`
	State     JobState     `json:"state"`
	Enabled   bool         `json:"enabled"`
	CreatedAt int64        `json:"created_at"`
	EntryID   cron.EntryID `json:"-"`
}

type JobHandler func(job *CronJob) (string, error)

type CronService struct {
	cron      *cron.Cron
	jobs      map[string]*CronJob
	storePath string
	mu        sync.RWMutex
	onJob     JobHandler
}

func NewCronService(storePath string, l *logger.Logger) *CronService {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(storePath), 0755); err != nil {
		fmt.Printf("Failed to create cron store directory: %v\n", err)
	}

	s := &CronService{
		cron:      cron.New(cron.WithSeconds()),
		jobs:      make(map[string]*CronJob),
		storePath: storePath,
	}

	// Load jobs from store
	s.loadJobs()

	return s
}

func (s *CronService) SetOnJob(handler JobHandler) {
	s.onJob = handler
}

func (s *CronService) Start() error {
	s.cron.Start()
	// Re-schedule enabled jobs
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, job := range s.jobs {
		if job.Enabled {
			if err := s.scheduleJob(job); err != nil {
				logger.ErrorCF("cron", "Failed to schedule job", map[string]interface{}{
					"job":   job.Name,
					"error": err,
				})
			}
		}
	}
	logger.InfoC("cron", "Cron service started")
	return nil
}

func (s *CronService) Stop() {
	s.cron.Stop()
	logger.InfoC("cron", "Cron service stopped")
}

func (s *CronService) scheduleJob(job *CronJob) error {
	var scheduleStr string
	if job.Schedule.Kind == "cron" {
		scheduleStr = job.Schedule.Expr
	} else if job.Schedule.Kind == "every" {
		// convert ms to duration string
		if job.Schedule.EveryMS != nil {
			seconds := *job.Schedule.EveryMS / 1000
			scheduleStr = fmt.Sprintf("@every %ds", seconds)
		}
	} else if job.Schedule.Kind == "at" {
		if job.Schedule.AtMS != nil {
			targetTime := time.UnixMilli(*job.Schedule.AtMS)
			delay := time.Until(targetTime)
			if delay < 0 {
				// Past time, run immediately
				go s.runJob(job)
				// Remove job after execution since it's one-time
				// We do this in runJob wrapper if we want, or here.
				// But runJob is generic.
				// Let's just remove it here.
				// But we need to remove it from the map/store too.
				// Defer removal?
			} else {
				time.AfterFunc(delay, func() {
					s.runJob(job)
					// Remove job after execution since it's one-time
					s.RemoveJob(job.ID)
				})
			}
			return nil
		}
	}

	if scheduleStr != "" {
		entryID, err := s.cron.AddFunc(scheduleStr, func() {
			s.runJob(job)
		})
		if err != nil {
			return err
		}
		job.EntryID = entryID
	}
	return nil
}

func (s *CronService) runJob(job *CronJob) {
	s.mu.Lock()
	now := time.Now().UnixMilli()
	job.State.LastRunAtMS = &now
	job.State.RunCount++
	s.mu.Unlock()

	if s.onJob != nil {
		_, err := s.onJob(job)

		s.mu.Lock()
		if err != nil {
			job.State.Error = err.Error()
		} else {
			job.State.Error = ""
		}
		s.saveJobs()
		s.mu.Unlock()
	}
}

func (s *CronService) AddJob(name string, schedule CronSchedule, message string, deliver bool, channel, chatID string) (*CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := fmt.Sprintf("job-%d", time.Now().UnixNano())
	job := &CronJob{
		ID:   id,
		Name: name,
		Schedule: schedule,
		Payload: JobPayload{
			Message: message,
			Deliver: deliver,
			Channel: channel,
			To:      chatID,
		},
		Enabled:   true,
		CreatedAt: time.Now().Unix(),
	}

	if err := s.scheduleJob(job); err != nil {
		return nil, err
	}

	s.jobs[id] = job
	s.saveJobs()
	return job, nil
}

func (s *CronService) RemoveJob(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok {
		return false
	}

	if job.EntryID != 0 {
		s.cron.Remove(job.EntryID)
	}

	delete(s.jobs, id)
	s.saveJobs()
	return true
}

func (s *CronService) EnableJob(id string, enable bool) *CronJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok {
		return nil
	}

	if job.Enabled == enable {
		return job
	}

	job.Enabled = enable
	if enable {
		s.scheduleJob(job)
	} else {
		if job.EntryID != 0 {
			s.cron.Remove(job.EntryID)
			job.EntryID = 0
		}
	}

	s.saveJobs()
	return job
}

func (s *CronService) ListJobs(all bool) []*CronJob {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*CronJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		// Update NextRunAtMS if entry exists
		if job.EntryID != 0 {
			entry := s.cron.Entry(job.EntryID)
			if !entry.Next.IsZero() {
				nextMS := entry.Next.UnixMilli()
				job.State.NextRunAtMS = &nextMS
			}
		}
		list = append(list, job)
	}
	return list
}

func (s *CronService) UpdateJob(job *CronJob) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.jobs[job.ID]; ok {
		// Update fields
		existing.Payload = job.Payload
		// If schedule changed, we would need to reschedule, but for now assuming only payload updates from tool
		s.saveJobs()
	}
}

func (s *CronService) loadJobs() {
	data, err := os.ReadFile(s.storePath)
	if err != nil {
		return
	}

	var storedJobs []*CronJob
	if err := json.Unmarshal(data, &storedJobs); err != nil {
		return
	}

	for _, job := range storedJobs {
		s.jobs[job.ID] = job
	}
}

func (s *CronService) saveJobs() {
	jobs := make([]*CronJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}

	data, _ := json.MarshalIndent(jobs, "", "  ")
	os.WriteFile(s.storePath, data, 0644)
}
