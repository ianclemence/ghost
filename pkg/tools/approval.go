package tools

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ApprovalRequest represents a pending approval request.
type ApprovalRequest struct {
	ID          string     `json:"id"`
	Action      string     `json:"action"`
	Description string     `json:"description"`
	Requester   string     `json:"requester"` // Tool or agent requesting
	Channel     string     `json:"channel"`   // Channel to send approval to
	ChatID      string     `json:"chat_id"`   // Chat ID to send approval to
	Status      string     `json:"status"`    // "pending", "approved", "denied", "timeout"
	CreatedAt   time.Time  `json:"created_at"`
	RespondedAt *time.Time `json:"responded_at,omitempty"`
	Result      string     `json:"result"` // Approval result message
}

// ApprovalStore manages pending approval requests.
type ApprovalStore struct {
	requests map[string]*ApprovalRequest
	mu       sync.RWMutex
}

// ApprovalTool provides human-in-the-loop approval workflows.
type ApprovalTool struct {
	store      *ApprovalStore
	callbackCh chan ApprovalRequest
}

func NewApprovalTool() *ApprovalTool {
	return &ApprovalTool{
		store: &ApprovalStore{
			requests: make(map[string]*ApprovalRequest),
		},
		callbackCh: make(chan ApprovalRequest, 100),
	}
}

func (t *ApprovalTool) Name() string {
	return "approval"
}

func (t *ApprovalTool) Description() string {
	return "Request human approval before executing sensitive actions. Use for destructive operations, external API calls with side effects, or when human oversight is required."
}

func (t *ApprovalTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action to perform: request, approve, deny, check",
				"enum":        []string{"request", "approve", "deny", "check"},
			},
			"request_id": map[string]interface{}{
				"type":        "string",
				"description": "Request ID (for approve, deny, check)",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Description of action requiring approval",
			},
			"action_to_approve": map[string]interface{}{
				"type":        "string",
				"description": "The action or command to approve",
			},
			"result": map[string]interface{}{
				"type":        "string",
				"description": "Result message (for approve, deny)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *ApprovalTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	action, _ := args["action"].(string)
	if action == "" {
		return ErrorResult("action is required")
	}

	switch action {
	case "request":
		return t.requestApproval(ctx, args)
	case "approve":
		return t.respondToRequest(args, "approved")
	case "deny":
		return t.respondToRequest(args, "denied")
	case "check":
		return t.checkRequest(args)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %s", action))
	}
}

func (t *ApprovalTool) requestApproval(ctx context.Context, args map[string]interface{}) *ToolResult {
	description, _ := args["description"].(string)
	actionToApprove, _ := args["action_to_approve"].(string)

	if description == "" {
		return ErrorResult("description is required")
	}

	request := &ApprovalRequest{
		ID:          fmt.Sprintf("approval-%d", time.Now().UnixMilli()),
		Action:      actionToApprove,
		Description: description,
		Status:      "pending",
		CreatedAt:   time.Now(),
	}

	t.store.mu.Lock()
	t.store.requests[request.ID] = request
	t.store.mu.Unlock()

	// Send to callback channel for external handling
	select {
	case t.callbackCh <- *request:
	default:
	}

	return &ToolResult{
		ForLLM:  fmt.Sprintf("Approval request created: %s. Waiting for human approval.", request.ID),
		ForUser: fmt.Sprintf("Approval required:\n%s\n\nRequest ID: %s", description, request.ID),
		Silent:  false,
		IsError: false,
	}
}

func (t *ApprovalTool) respondToRequest(args map[string]interface{}, status string) *ToolResult {
	requestID, _ := args["request_id"].(string)
	if requestID == "" {
		return ErrorResult("request_id is required")
	}

	t.store.mu.Lock()
	defer t.store.mu.Unlock()

	request, ok := t.store.requests[requestID]
	if !ok {
		return ErrorResult(fmt.Sprintf("request %q not found", requestID))
	}

	if request.Status != "pending" {
		return ErrorResult(fmt.Sprintf("request %q already %s", requestID, request.Status))
	}

	request.Status = status
	now := time.Now()
	request.RespondedAt = &now
	if result, ok := args["result"].(string); ok {
		request.Result = result
	}

	return UserResult(fmt.Sprintf("Request %s: %s", requestID, status))
}

func (t *ApprovalTool) checkRequest(args map[string]interface{}) *ToolResult {
	requestID, _ := args["request_id"].(string)
	if requestID == "" {
		return ErrorResult("request_id is required")
	}

	t.store.mu.RLock()
	defer t.store.mu.RUnlock()

	request, ok := t.store.requests[requestID]
	if !ok {
		return ErrorResult(fmt.Sprintf("request %q not found", requestID))
	}

	result := fmt.Sprintf("Request %s:\nStatus: %s\nDescription: %s", request.ID, request.Status, request.Description)
	if request.Result != "" {
		result += fmt.Sprintf("\nResult: %s", request.Result)
	}

	return UserResult(result)
}

// GetPendingRequests returns all pending approval requests.
func (t *ApprovalTool) GetPendingRequests() []*ApprovalRequest {
	t.store.mu.RLock()
	defer t.store.mu.RUnlock()

	var pending []*ApprovalRequest
	for _, req := range t.store.requests {
		if req.Status == "pending" {
			pending = append(pending, req)
		}
	}
	return pending
}

// GetApprovalChannel returns the channel for approval callbacks.
func (t *ApprovalTool) GetApprovalChannel() <-chan ApprovalRequest {
	return t.callbackCh
}

// CleanupOldRequests removes requests older than the specified duration.
func (t *ApprovalTool) CleanupOldRequests(maxAge time.Duration) int {
	t.store.mu.Lock()
	defer t.store.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	removed := 0

	for id, req := range t.store.requests {
		if req.CreatedAt.Before(cutoff) && req.Status == "pending" {
			req.Status = "timeout"
			delete(t.store.requests, id)
			removed++
		}
	}

	return removed
}
