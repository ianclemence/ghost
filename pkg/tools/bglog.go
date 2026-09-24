package tools

import (
	"strconv"
	"sync"
	"time"
)

// maxBackgroundResultChars bounds stored completion text: the log is an
// index for delivery, not an archive. Full results already travel the
// governed paths (bus announce, session history).
const maxBackgroundResultChars = 4000

// BackgroundEventType names the lifecycle observations the log emits.
const (
	BackgroundEventStarted = "background_started"
	BackgroundEventDone    = "background_done"
)

// BackgroundEvent is one lifecycle observation: a detached task started
// or finished. Sinks (WS bridge, status surfaces) subscribe; the log
// itself stays presentation-free.
type BackgroundEvent struct {
	Type      string
	Session   string
	Label     string
	Tool      string
	OK        bool
	Result    string
	ElapsedMs int64
}

// BackgroundTask is one in-flight detached execution.
type BackgroundTask struct {
	ID        string
	Label     string
	Tool      string
	Session   string
	StartedAt time.Time
}

// BackgroundDone is a finished detached execution awaiting delivery.
type BackgroundDone struct {
	BackgroundTask
	OK         bool
	Result     string
	Elapsed    time.Duration
	FinishedAt time.Time
}

// BackgroundLog tracks detached (Async) tool executions per session so any
// surface can show "still running" state and deliver completions. It is
// purely observational: authority still lives with the broker, execution
// with the tool. Completions are drained exactly once (DrainBackgroundDone)
// so two surfaces never double-report.
type BackgroundLog struct {
	mu      sync.Mutex
	seq     uint64
	running map[string][]BackgroundTask
	done    map[string][]BackgroundDone
	// sink observes starts and finishes (WS bridge, status surfaces).
	// Nil = log only. Set once by the owning runtime.
	sink func(BackgroundEvent)
}

// SetEventSink installs the lifecycle observer. Nil disables.
func (l *BackgroundLog) SetEventSink(fn func(BackgroundEvent)) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sink = fn
}

// NewBackgroundLog returns an empty log.
func NewBackgroundLog() *BackgroundLog {
	return &BackgroundLog{
		running: map[string][]BackgroundTask{},
		done:    map[string][]BackgroundDone{},
	}
}

// Start records a detached execution. The returned ID keys its Finish.
func (l *BackgroundLog) Start(session, tool, label string) string {
	if l == nil {
		return ""
	}
	if label == "" {
		label = tool
	}
	l.mu.Lock()
	l.seq++
	id := "bg-" + strconv.FormatUint(l.seq, 10)
	t := BackgroundTask{ID: id, Label: label, Tool: tool, Session: session, StartedAt: time.Now().UTC()}
	sink := l.sink
	l.running[session] = append(l.running[session], t)
	l.mu.Unlock()
	if sink != nil {
		sink(BackgroundEvent{Type: BackgroundEventStarted, Session: session, Label: t.Label, Tool: tool})
	}
	return id
}

// Unstart removes a running entry that finished inline (a tool that
// implements AsyncTool but returned a synchronous result).
func (l *BackgroundLog) Unstart(session, id string) {
	if l == nil || id == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.running[session][:0]
	for _, t := range l.running[session] {
		if t.ID != id {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.running, session)
	} else {
		l.running[session] = kept
	}
}

// Finish moves a running entry to the done queue, truncating the result.
func (l *BackgroundLog) Finish(session, id string, ok bool, result string) {
	if l == nil || id == "" {
		return
	}
	if len(result) > maxBackgroundResultChars {
		result = result[:maxBackgroundResultChars] + "…"
	}
	now := time.Now().UTC()
	l.mu.Lock()
	var task BackgroundTask
	found := false
	kept := l.running[session][:0]
	for _, t := range l.running[session] {
		if t.ID == id {
			task, found = t, true
			continue
		}
		kept = append(kept, t)
	}
	if len(kept) == 0 {
		delete(l.running, session)
	} else {
		l.running[session] = kept
	}
	var finished BackgroundDone
	if found {
		finished = BackgroundDone{
			BackgroundTask: task, OK: ok, Result: result,
			Elapsed: now.Sub(task.StartedAt), FinishedAt: now,
		}
		l.done[session] = append(l.done[session], finished)
	}
	sink := l.sink
	l.mu.Unlock()
	if found && sink != nil {
		sink(BackgroundEvent{
			Type: BackgroundEventDone, Session: session, Label: task.Label,
			Tool: task.Tool, OK: ok, Result: result,
			ElapsedMs: finished.Elapsed.Milliseconds(),
		})
	}
}

// Running returns a copy of the session's in-flight tasks.
func (l *BackgroundLog) Running(session string) []BackgroundTask {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]BackgroundTask{}, l.running[session]...)
}

// DrainDone returns and clears the session's finished tasks.
func (l *BackgroundLog) DrainDone(session string) []BackgroundDone {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.done[session]
	delete(l.done, session)
	return out
}
