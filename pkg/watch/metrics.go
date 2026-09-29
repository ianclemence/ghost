package watch

import (
	"sync"
	"time"
)

// Metrics is the in-process counter set for watch activity. It answers the
// observability questions that a single watch record cannot: how much
// background work is happening, how often the world actually moved, and how
// often Ghost stayed quiet (and why). Counters are process-local — durable
// truth lives in the store; this is the rate, not the record.
type Metrics struct {
	mu sync.Mutex

	Created   int
	Cancelled int
	Expired   int
	Completed int
	Failed    int

	Probes        int
	ProbeFailures int
	Deferred      int // polls skipped because the daily budget was spent
	Changes       int
	Noticed       int // notices handed to the noticer
	Delivered     int // notices that reached the owner
	Suppressed    int

	SuppressedBy map[string]int

	LastProbeAt  time.Time
	LastChangeAt time.Time
	LastNoticeAt time.Time
	LastError    string
	LastErrorAt  time.Time
}

// NewMetrics returns zeroed metrics with the reason map ready.
func NewMetrics() *Metrics {
	return &Metrics{SuppressedBy: map[string]int{}}
}

func (m *Metrics) add(fn func(*Metrics)) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(m)
	if m.SuppressedBy == nil {
		m.SuppressedBy = map[string]int{}
	}
}

// RecordCreated counts a watch created.
func (m *Metrics) RecordCreated() { m.add(func(x *Metrics) { x.Created++ }) }

// RecordCancelled counts a watch cancelled by the owner.
func (m *Metrics) RecordCancelled() { m.add(func(x *Metrics) { x.Cancelled++ }) }

// RecordExpired counts a watch swept past its horizon.
func (m *Metrics) RecordExpired() { m.add(func(x *Metrics) { x.Expired++ }) }

// RecordCompleted counts a watch that reached a natural end.
func (m *Metrics) RecordCompleted() { m.add(func(x *Metrics) { x.Completed++ }) }

// RecordFailed counts a watch that exhausted its retries.
func (m *Metrics) RecordFailed() { m.add(func(x *Metrics) { x.Failed++ }) }

// RecordProbe counts one probe attempt (successful or not).
func (m *Metrics) RecordProbe(at time.Time) {
	m.add(func(x *Metrics) { x.Probes++; x.LastProbeAt = at })
}

// RecordProbeFailure counts a failed probe and keeps the reason.
func (m *Metrics) RecordProbeFailure(reason string) {
	m.add(func(x *Metrics) {
		x.ProbeFailures++
		x.LastError = truncate(reason, 200)
		x.LastErrorAt = time.Now().UTC()
	})
}

// RecordDeferred counts a poll skipped because the budget was spent.
func (m *Metrics) RecordDeferred() { m.add(func(x *Metrics) { x.Deferred++ }) }

// RecordChanges counts detected changes.
func (m *Metrics) RecordChanges(n int) {
	if n <= 0 {
		return
	}
	m.add(func(x *Metrics) { x.Changes += n; x.LastChangeAt = time.Now().UTC() })
}

// RecordNoticed counts a notice handed to the noticer.
func (m *Metrics) RecordNoticed() {
	m.add(func(x *Metrics) { x.Noticed++; x.LastNoticeAt = time.Now().UTC() })
}

// RecordDelivered counts a notice that reached the owner.
func (m *Metrics) RecordDelivered() { m.add(func(x *Metrics) { x.Delivered++ }) }

// RecordSuppressed counts a notice held back, attributed to a reason.
func (m *Metrics) RecordSuppressed(reason string) {
	m.add(func(x *Metrics) {
		x.Suppressed++
		if reason == "" {
			reason = "unknown"
		}
		x.SuppressedBy[reason]++
	})
}

// Snapshot is an immutable copy for status surfaces and tests.
type MetricsSnapshot struct {
	Created, Cancelled, Expired, Completed, Failed int
	Probes, ProbeFailures, Deferred                int
	Changes, Noticed, Delivered, Suppressed        int
	SuppressedBy                                   map[string]int
	LastProbeAt, LastChangeAt, LastNoticeAt        time.Time
	LastError                                      string
	LastErrorAt                                    time.Time
}

// Snapshot copies the current counters.
func (m *Metrics) Snapshot() MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{SuppressedBy: map[string]int{}}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	sup := map[string]int{}
	for k, v := range m.SuppressedBy {
		sup[k] = v
	}
	return MetricsSnapshot{
		Created: m.Created, Cancelled: m.Cancelled, Expired: m.Expired,
		Completed: m.Completed, Failed: m.Failed,
		Probes: m.Probes, ProbeFailures: m.ProbeFailures, Deferred: m.Deferred,
		Changes: m.Changes, Noticed: m.Noticed, Delivered: m.Delivered,
		Suppressed: m.Suppressed, SuppressedBy: sup,
		LastProbeAt: m.LastProbeAt, LastChangeAt: m.LastChangeAt,
		LastNoticeAt: m.LastNoticeAt, LastError: m.LastError,
		LastErrorAt: m.LastErrorAt,
	}
}
