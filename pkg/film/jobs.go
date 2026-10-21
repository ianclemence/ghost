package film

import (
	"context"
	"path/filepath"
	"sync"
	"time"
)

// Job is one render, as the owner's conversation sees it.
type Job struct {
	File    string  `json:"file"`
	State   string  `json:"state"` // none, queued, rendering, done, failed
	Done    int     `json:"done"`
	Total   int     `json:"total"`
	Error   string  `json:"error,omitempty"`
	Video   string  `json:"video,omitempty"`
	Contact string  `json:"contact,omitempty"`
	Audio   bool    `json:"audio,omitempty"`
	Seconds float64 `json:"seconds,omitempty"`
}

// Jobs renders one film at a time (a Pod has one browser's worth of room) and
// remembers how each went.
type Jobs struct {
	mu   sync.Mutex
	jobs map[string]*Job
	sem  chan struct{}
}

func NewJobs() *Jobs { return &Jobs{jobs: map[string]*Job{}, sem: make(chan struct{}, 1)} }

// Start begins rendering the film, unless it is already being rendered. done
// is called once, when the render ends either way.
func (x *Jobs) Start(workspace, file string, o Options, done func(Job)) Job {
	x.mu.Lock()
	if j, ok := x.jobs[file]; ok && (j.State == "queued" || j.State == "rendering") {
		out := *j
		x.mu.Unlock()
		return out
	}
	j := &Job{File: file, State: "queued"}
	x.jobs[file] = j
	x.mu.Unlock()
	go func() {
		x.sem <- struct{}{}
		defer func() { <-x.sem }()
		x.set(file, func(j *Job) { j.State = "rendering" })
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()
		res, err := Render(ctx, workspace, file, o, func(d, t int) {
			x.set(file, func(j *Job) { j.Done, j.Total = d, t })
		})
		x.set(file, func(j *Job) {
			if err != nil {
				j.State, j.Error = "failed", err.Error()
				return
			}
			j.State, j.Video, j.Contact, j.Audio = "done", res.Video, res.Contact, res.Audio
			j.Done, j.Total, j.Seconds = res.Meta.Frames, res.Meta.Frames, res.Meta.Seconds()
		})
		if done != nil {
			done(x.Status(file))
		}
	}()
	return x.Status(file)
}

func (x *Jobs) set(file string, fn func(j *Job)) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if j, ok := x.jobs[file]; ok {
		fn(j)
	}
}

// Status is how the render for file is going (state "none" if never asked).
func (x *Jobs) Status(file string) Job {
	x.mu.Lock()
	defer x.mu.Unlock()
	if j, ok := x.jobs[file]; ok {
		return *j
	}
	return Job{File: file, State: "none"}
}

var (
	jobsMu sync.Mutex
	jobsBy = map[string]*Jobs{}
)

// JobsFor is the one renderer of a workspace, so a film asked for twice is
// rendered once.
func JobsFor(workspace string) *Jobs {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	key := filepath.Clean(workspace)
	if x, ok := jobsBy[key]; ok {
		return x
	}
	x := NewJobs()
	jobsBy[key] = x
	return x
}
