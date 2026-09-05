// Package setup implements the Ghost provisioning/setup orchestrator.
//
// One coherent lifecycle replaces scattered installation assumptions:
//
//	Ghost Setup: Runtime, Storage, Security, Local AI, Configuration,
//	Networking, Remote Access, Integrations, Backup, Updates, Diagnostics.
//
// Appliance states: NOT_INITIALIZED, INITIALIZING, READY, DEGRADED,
// ACTION_REQUIRED, RECOVERING.
//
// Every step is idempotent: inspect -> determine state -> perform missing
// operation -> validate -> persist checkpoint. Running setup twice never
// destroys state; interrupted setup (reboot, network loss, failed model
// download, partial config) resumes from the last checkpoint. Existing
// owner data and credentials are never blindly overwritten.
package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// State is the appliance lifecycle state.
type State string

const (
	NotInitialized State = "not_initialized"
	Initializing   State = "initializing"
	Ready          State = "ready"
	Degraded       State = "degraded"
	ActionRequired State = "action_required"
	Recovering     State = "recovering"
)

// StepID identifies a provisioning step.
type StepID string

const (
	StepRuntime       StepID = "runtime"
	StepStorage       StepID = "storage"
	StepSecurity      StepID = "security"
	StepLocalAI       StepID = "local_ai"
	StepConfiguration StepID = "configuration"
	StepNetworking    StepID = "networking"
	StepRemoteAccess  StepID = "remote_access"
	StepIntegrations  StepID = "integrations"
	StepBackup        StepID = "backup"
	StepUpdates       StepID = "updates"
	StepDiagnostics   StepID = "diagnostics"
)

// OrderedSteps is the canonical provisioning order.
var OrderedSteps = []StepID{StepRuntime, StepStorage, StepSecurity, StepLocalAI, StepConfiguration, StepNetworking, StepRemoteAccess, StepIntegrations, StepBackup, StepUpdates, StepDiagnostics}

// StepStatus is the per-step checkpoint state.
type StepStatus string

const (
	StepPending StepStatus = "pending"
	StepRunning StepStatus = "running"
	StepDone    StepStatus = "done"
	StepFailed  StepStatus = "failed"
	StepSkipped StepStatus = "skipped"
)

// Step is one idempotent provisioning operation. Check reports whether
// the step's goal already holds (safe to skip); Apply performs only the
// missing work; Validate confirms the goal now holds.
type Step struct {
	ID       StepID
	Human    string // product-language name, e.g. "Local AI"
	Required bool   // required steps gate READY; optional gate DEGRADED at worst
	Check    func() (bool, string)
	Apply    func() error
	Validate func() error
}

// Checkpoint persists progress so interrupted setup resumes.
type Checkpoint struct {
	Steps     map[StepID]StepStatus `json:"steps"`
	Notes     map[StepID]string     `json:"notes,omitempty"`
	UpdatedAt time.Time             `json:"updated_at"`
}

// Orchestrator runs steps idempotently with checkpointing.
type Orchestrator struct {
	mu    sync.Mutex
	dir   string
	steps map[StepID]Step
	state State
	cp    Checkpoint
}

// New creates an orchestrator rooted at dir (checkpoints at
// <dir>/setup-checkpoint.json). It loads any prior checkpoint so a reboot
// during setup resumes instead of restarting.
func New(dir string, steps []Step) (*Orchestrator, error) {
	o := &Orchestrator{
		dir:   dir,
		steps: map[StepID]Step{},
		state: NotInitialized,
		cp:    Checkpoint{Steps: map[StepID]StepStatus{}, Notes: map[StepID]string{}},
	}
	for _, s := range steps {
		o.steps[s.ID] = s
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if data, err := os.ReadFile(filepath.Join(dir, "setup-checkpoint.json")); err == nil {
		var cp Checkpoint
		if json.Unmarshal(data, &cp) == nil {
			if cp.Steps == nil {
				cp.Steps = map[StepID]StepStatus{}
			}
			if cp.Notes == nil {
				cp.Notes = map[StepID]string{}
			}
			o.cp = cp
			o.state = o.deriveState()
		}
	}
	return o, nil
}

func (o *Orchestrator) persist() error {
	o.cp.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(o.cp, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(o.dir, "setup-checkpoint.json.tmp")
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(o.dir, "setup-checkpoint.json"))
}

func (o *Orchestrator) deriveState() State {
	anyFailedRequired := false
	anyFailedOptional := false
	anyStarted := false
	allTerminal := len(o.steps) > 0
	for id, st := range o.steps {
		s := o.cp.Steps[id]
		switch s {
		case StepDone, StepSkipped:
			anyStarted = true
		case StepFailed:
			anyStarted = true
			if st.Required {
				anyFailedRequired = true
			} else {
				anyFailedOptional = true
			}
		case StepRunning:
			anyStarted = true
			allTerminal = false
		default: // pending / unknown
			allTerminal = false
		}
	}
	switch {
	case allTerminal:
		if anyFailedRequired {
			return ActionRequired
		}
		if anyFailedOptional {
			return Degraded
		}
		return Ready
	case anyFailedRequired:
		return ActionRequired
	case anyStarted:
		return Recovering
	default:
		return NotInitialized
	}
}

// Status returns the current appliance state.
func (o *Orchestrator) Status() State {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.deriveState()
}

// StepState returns a step's checkpoint.
func (o *Orchestrator) StepState(id StepID) StepStatus {
	o.mu.Lock()
	defer o.mu.Unlock()
	if s, ok := o.cp.Steps[id]; ok {
		return s
	}
	return StepPending
}

// Run executes all steps in canonical order, skipping steps whose Check
// already passes (idempotency) and resuming past checkpoints. It never
// overwrites completed work. First failure of a required step stops the
// run; optional failures are recorded and the run continues.
func (o *Orchestrator) Run() error {
	o.mu.Lock()
	o.state = Initializing
	o.mu.Unlock()
	var firstErr error
	for _, id := range OrderedSteps {
		st, ok := o.steps[id]
		if !ok {
			continue
		}
		o.mu.Lock()
		if o.cp.Steps[id] == StepDone {
			// Re-verify: a checkpoint claiming done must still hold.
			o.mu.Unlock()
			if ok2, _ := st.Check(); ok2 {
				continue
			}
			o.mu.Lock()
			o.cp.Steps[id] = StepPending
			o.persist()
			o.mu.Unlock()
		} else {
			o.mu.Unlock()
		}
		// Idempotent fast path: goal already holds -> mark done.
		if done, note := st.Check(); done {
			o.mu.Lock()
			o.cp.Steps[id] = StepDone
			if note != "" {
				o.cp.Notes[id] = note
			}
			o.persist()
			o.mu.Unlock()
			continue
		}
		o.mu.Lock()
		o.cp.Steps[id] = StepRunning
		o.persist()
		o.mu.Unlock()
		if err := st.Apply(); err != nil {
			o.mu.Lock()
			o.cp.Steps[id] = StepFailed
			o.cp.Notes[id] = err.Error()
			o.persist()
			o.mu.Unlock()
			if st.Required && firstErr == nil {
				firstErr = fmt.Errorf("step %s: %w", id, err)
				break
			}
			if firstErr == nil && !st.Required {
				firstErr = fmt.Errorf("step %s: %w", id, err)
			}
			continue
		}
		if st.Validate != nil {
			if err := st.Validate(); err != nil {
				o.mu.Lock()
				o.cp.Steps[id] = StepFailed
				o.cp.Notes[id] = "validation: " + err.Error()
				o.persist()
				o.mu.Unlock()
				if st.Required && firstErr == nil {
					firstErr = fmt.Errorf("step %s validation: %w", id, err)
					break
				}
				continue
			}
		}
		o.mu.Lock()
		o.cp.Steps[id] = StepDone
		o.persist()
		o.mu.Unlock()
	}
	o.mu.Lock()
	o.state = o.deriveState()
	o.mu.Unlock()
	return firstErr
}

// ResetStep clears one checkpoint so the step runs again (for reconnect /
// retry flows). It never touches other steps or owner data.
func (o *Orchestrator) ResetStep(id StepID) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.cp.Steps, id)
	delete(o.cp.Notes, id)
	o.persist()
}
