package tools

import "context"

// Tool is the unit of capability: name, model-facing description,
// JSON parameters, and execution.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]interface{}
	Execute(ctx context.Context, args map[string]interface{}) *ToolResult
}

// ContextualTool is an optional interface that tools can implement
// to receive the current message context (channel, chatID)
type ContextualTool interface {
	Tool
	SetContext(channel, chatID string)
}

// AsyncCallback notifies completion of an async tool. It runs on a
// goroutine and must be thread-safe; ctx is canceled on agent shutdown.
type AsyncCallback func(ctx context.Context, result *ToolResult)

// AsyncTool is implemented by tools whose Execute returns immediately
// with an AsyncResult and reports completion later via SetCallback.
type AsyncTool interface {
	Tool
	// SetCallback registers a callback function to be invoked when the async operation completes.
	// The callback will be called from a goroutine and should handle thread-safety if needed.
	SetCallback(cb AsyncCallback)
}

func ToolToSchema(tool Tool) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        tool.Name(),
			"description": tool.Description(),
			"parameters":  tool.Parameters(),
		},
	}
}
