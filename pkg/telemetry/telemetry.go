package telemetry

import (
	"sync"
	"time"
)

// TraceEvent represents a single step in a message's lifecycle.
type TraceEvent struct {
	RequestID string `json:"request_id"`
	State     string `json:"state"`
	At        int64  `json:"at"`
	Channel   string `json:"channel,omitempty"`
	ChatID    string `json:"chat_id,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// ChannelIncident tracks failures for a specific communication channel.
type ChannelIncident struct {
	Channel      string `json:"channel"`
	FailureCount int    `json:"failure_count"`
	LastError    string `json:"last_error"`
	LastAt       int64  `json:"last_at"`
}

// Manager handles the storage and retrieval of telemetry data.
type Manager struct {
	mu                   sync.RWMutex
	traceByRequest       map[string][]TraceEvent
	lastRequestBySession map[string]string
	channelIncidents     map[string]ChannelIncident
	maxTracesPerRequest  int
}

// Global is the default telemetry manager instance.
var Global = NewManager()

// NewManager creates a new telemetry manager.
func NewManager() *Manager {
	return &Manager{
		traceByRequest:       make(map[string][]TraceEvent),
		lastRequestBySession: make(map[string]string),
		channelIncidents:     make(map[string]ChannelIncident),
		maxTracesPerRequest:  100,
	}
}

// Record adds a new trace event to the manager.
func (m *Manager) Record(session, reqID, state, channel, chatID, detail string) {
	if reqID == "" {
		return
	}

	event := TraceEvent{
		RequestID: reqID,
		State:     state,
		At:        time.Now().Unix(),
		Channel:   channel,
		ChatID:    chatID,
		Detail:    detail,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	traces := m.traceByRequest[reqID]
	if len(traces) >= m.maxTracesPerRequest {
		// Keep it bounded
		traces = traces[1:]
	}
	m.traceByRequest[reqID] = append(traces, event)

	if session != "" {
		m.lastRequestBySession[session] = reqID
	}
}

// RecordIncident logs a failure for a specific channel.
func (m *Manager) RecordIncident(channel, errText string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	incident := m.channelIncidents[channel]
	incident.Channel = channel
	incident.FailureCount++
	incident.LastError = errText
	incident.LastAt = time.Now().Unix()
	m.channelIncidents[channel] = incident
}

// ClearIncidents resets the failure count for a channel.
func (m *Manager) ClearIncidents(channel string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.channelIncidents, channel)
}

// GetTraces returns all events for a specific request.
func (m *Manager) GetTraces(reqID string) []TraceEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	traces := m.traceByRequest[reqID]
	if traces == nil {
		return []TraceEvent{}
	}
	
	// Return a copy to avoid data races
	cp := make([]TraceEvent, len(traces))
	copy(cp, traces)
	return cp
}

// GetLastRequestID returns the most recent request ID for a session.
func (m *Manager) GetLastRequestID(session string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastRequestBySession[session]
}

// GetIncidents returns all current channel incidents.
func (m *Manager) GetIncidents() map[string]ChannelIncident {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	cp := make(map[string]ChannelIncident, len(m.channelIncidents))
	for k, v := range m.channelIncidents {
		cp[k] = v
	}
	return cp
}

// GetTraceBySession returns traces for the most recent request in a session.
func (m *Manager) GetTraceBySession(session string) (string, []TraceEvent) {
	m.mu.RLock()
	reqID := m.lastRequestBySession[session]
	m.mu.RUnlock()
	
	if reqID == "" {
		return "", []TraceEvent{}
	}
	
	return reqID, m.GetTraces(reqID)
}
