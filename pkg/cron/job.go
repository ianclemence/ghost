package cron

import "time"

type JobState string

const (
	JobStateActive  JobState = "active"
	JobStatePaused  JobState = "paused"
	JobStateRunning JobState = "running"
)

type JobUpdate struct {
	Name     *string       `json:"name,omitempty"`
	Schedule *CronSchedule `json:"schedule,omitempty"`
	Message  *string       `json:"message,omitempty"`
	Command  *string       `json:"command,omitempty"`
	Deliver  *bool         `json:"deliver,omitempty"`
	Channel  *string       `json:"channel,omitempty"`
	To       *string       `json:"to,omitempty"`
	Target   *string       `json:"target,omitempty"`
	Enabled  *bool         `json:"enabled,omitempty"`
}

type JobStatus struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	State     JobState   `json:"state"`
	Enabled   bool       `json:"enabled"`
	RunCount  int        `json:"run_count"`
	LastRunAt *time.Time `json:"last_run_at,omitempty"`
	NextRunAt *time.Time `json:"next_run_at,omitempty"`
	LastError string     `json:"last_error,omitempty"`
}
