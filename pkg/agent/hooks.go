package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
)

// ToolApprovalAction represents the decision from a tool approver.
type ToolApprovalAction int

const (
	ToolApprovalAllow ToolApprovalAction = iota
	ToolApprovalDeny
)

// String returns the string representation of the action.
func (a ToolApprovalAction) String() string {
	switch a {
	case ToolApprovalAllow:
		return "allow"
	case ToolApprovalDeny:
		return "deny"
	default:
		return "unknown"
	}
}

// ToolApprovalRequest contains information about a tool execution request.
type ToolApprovalRequest struct {
	ToolName   string                 `json:"tool_name"`
	Args       map[string]interface{} `json:"args"`
	Channel    string                 `json:"channel"`
	ChatID     string                 `json:"chat_id"`
	SessionKey string                 `json:"session_key"`
	Timestamp  time.Time              `json:"timestamp"`
}

// ToolApprovalDecision represents the result of an approval check.
type ToolApprovalDecision struct {
	Action ToolApprovalAction `json:"action"`
	Reason string             `json:"reason,omitempty"`
}

// ToolApprover is the interface that tool approval hooks must implement.
// External code (in-process or subprocess) can implement this to gate tool execution.
type ToolApprover interface {
	// ApproveTool is called before a tool is executed.
	// It returns a decision to allow or deny the tool execution.
	ApproveTool(ctx context.Context, req ToolApprovalRequest) ToolApprovalDecision
}

// ToolApproverFunc is a convenience type for implementing ToolApprover as a function.
type ToolApproverFunc func(ctx context.Context, req ToolApprovalRequest) ToolApprovalDecision

// ApproveTool implements the ToolApprover interface.
func (f ToolApproverFunc) ApproveTool(ctx context.Context, req ToolApprovalRequest) ToolApprovalDecision {
	return f(ctx, req)
}

// HookManager manages tool approvers and dispatches approval requests.
type HookManager struct {
	approvers []hookEntry
	mu        sync.RWMutex
}

type hookEntry struct {
	name     string
	priority int
	approver ToolApprover
}

// NewHookManager creates a new HookManager.
func NewHookManager() *HookManager {
	return &HookManager{
		approvers: make([]hookEntry, 0),
	}
}

// RegisterToolApprover registers a tool approver with a name and priority.
// Lower priority numbers are evaluated first.
func (hm *HookManager) RegisterToolApprover(name string, priority int, approver ToolApprover) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	// Remove existing entry with same name
	for i, entry := range hm.approvers {
		if entry.name == name {
			hm.approvers = append(hm.approvers[:i], hm.approvers[i+1:]...)
			break
		}
	}

	hm.approvers = append(hm.approvers, hookEntry{
		name:     name,
		priority: priority,
		approver: approver,
	})

	// Sort by priority (lower = higher priority)
	for i := 1; i < len(hm.approvers); i++ {
		for j := i; j > 0 && hm.approvers[j].priority < hm.approvers[j-1].priority; j-- {
			hm.approvers[j], hm.approvers[j-1] = hm.approvers[j-1], hm.approvers[j]
		}
	}

	logger.InfoCF("hooks", "Tool approver registered", map[string]interface{}{
		"name":     name,
		"priority": priority,
	})
}

// RemoveToolApprover removes a registered tool approver by name.
func (hm *HookManager) RemoveToolApprover(name string) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	for i, entry := range hm.approvers {
		if entry.name == name {
			hm.approvers = append(hm.approvers[:i], hm.approvers[i+1:]...)
			logger.InfoCF("hooks", "Tool approver removed", map[string]interface{}{
				"name": name,
			})
			return
		}
	}
}

// CheckToolApproval dispatches a tool approval request to all registered approvers.
// It returns the first deny decision, or allows if all approvers allow.
func (hm *HookManager) CheckToolApproval(ctx context.Context, req ToolApprovalRequest) ToolApprovalDecision {
	hm.mu.RLock()
	approvers := make([]hookEntry, len(hm.approvers))
	copy(approvers, hm.approvers)
	hm.mu.RUnlock()

	for _, entry := range approvers {
		decision := entry.approver.ApproveTool(ctx, req)
		if decision.Action == ToolApprovalDeny {
			logger.WarnCF("hooks", "Tool execution denied by approver", map[string]interface{}{
				"approver": entry.name,
				"tool":     req.ToolName,
				"reason":   decision.Reason,
			})
			return decision
		}
	}

	return ToolApprovalDecision{Action: ToolApprovalAllow}
}

// HasApprovers returns true if any tool approvers are registered.
func (hm *HookManager) HasApprovers() bool {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return len(hm.approvers) > 0
}

// ListApprovers returns the names of all registered approvers.
func (hm *HookManager) ListApprovers() []string {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	names := make([]string, len(hm.approvers))
	for i, entry := range hm.approvers {
		names[i] = entry.name
	}
	return names
}

// DenyPatternToolApprover is a built-in approver that denies tools matching specific patterns.
type DenyPatternToolApprover struct {
	denyPatterns []string
}

// NewDenyPatternToolApprover creates a DenyPatternToolApprover with the given patterns.
// Patterns are matched against "toolName argKey=argValue" strings.
func NewDenyPatternToolApprover(patterns []string) *DenyPatternToolApprover {
	return &DenyPatternToolApprover{
		denyPatterns: patterns,
	}
}

// ApproveTool checks if the tool execution matches any deny pattern.
func (d *DenyPatternToolApprover) ApproveTool(ctx context.Context, req ToolApprovalRequest) ToolApprovalDecision {
	// Build a searchable string from the request
	var sb strings.Builder
	sb.WriteString(req.ToolName)
	for k, v := range req.Args {
		sb.WriteString(fmt.Sprintf(" %s=%v", k, v))
	}
	searchStr := strings.ToLower(sb.String())

	for _, pattern := range d.denyPatterns {
		if strings.Contains(searchStr, strings.ToLower(pattern)) {
			return ToolApprovalDecision{
				Action: ToolApprovalDeny,
				Reason: fmt.Sprintf("matches deny pattern: %s", pattern),
			}
		}
	}

	return ToolApprovalDecision{Action: ToolApprovalAllow}
}

// ToolDenyList is a simple approver that denies specific tool names.
type ToolDenyList struct {
	deniedTools map[string]string // tool name -> reason
}

// NewToolDenyList creates a ToolDenyList from a map of tool names to deny reasons.
func NewToolDenyList(deniedTools map[string]string) *ToolDenyList {
	return &ToolDenyList{
		deniedTools: deniedTools,
	}
}

// ApproveTool checks if the tool is in the deny list.
func (td *ToolDenyList) ApproveTool(ctx context.Context, req ToolApprovalRequest) ToolApprovalDecision {
	if reason, denied := td.deniedTools[req.ToolName]; denied {
		return ToolApprovalDecision{
			Action: ToolApprovalDeny,
			Reason: reason,
		}
	}
	return ToolApprovalDecision{Action: ToolApprovalAllow}
}

// AuditLogToolApprover is an approver that logs all tool calls and always allows them.
type AuditLogToolApprover struct {
	mu    sync.Mutex
	logs  []ToolApprovalRequest
	entry int
}

// NewAuditLogToolApprover creates a new AuditLogToolApprover.
func NewAuditLogToolApprover() *AuditLogToolApprover {
	return &AuditLogToolApprover{
		logs: make([]ToolApprovalRequest, 0, 100),
	}
}

// ApproveTool logs the tool request and allows it.
func (a *AuditLogToolApprover) ApproveTool(ctx context.Context, req ToolApprovalRequest) ToolApprovalDecision {
	a.mu.Lock()
	a.logs = append(a.logs, req)
	a.entry++
	if len(a.logs) > 1000 {
		a.logs = a.logs[len(a.logs)-500:]
	}
	a.mu.Unlock()

	logger.InfoCF("hooks", "Tool call audited", map[string]interface{}{
		"tool":     req.ToolName,
		"channel":  req.Channel,
		"session":  req.SessionKey,
		"entry_no": a.entry,
	})

	return ToolApprovalDecision{Action: ToolApprovalAllow}
}

// GetLogs returns a copy of the audit logs.
func (a *AuditLogToolApprover) GetLogs() []ToolApprovalRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]ToolApprovalRequest, len(a.logs))
	copy(result, a.logs)
	return result
}

// Count returns the total number of audited tool calls.
func (a *AuditLogToolApprover) Count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.entry
}
