package sting

// ToolSchema is the wire shape the sidecar accepts per tool: the same
// contract as Ghost's provider ToolDefinition (type/function/name/
// description/parameters as JSON Schema), kept local so this package
// stays dependency-free.
type ToolSchema struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// FunctionCall is one proposed call from the router.
type FunctionCall struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// CompleteRequest is POSTed to the sidecar /complete endpoint.
type CompleteRequest struct {
	System string       `json:"system,omitempty"`
	Query  string       `json:"query"`
	Tools  []ToolSchema `json:"tools"`
	// Weights names a tuned .cact on the sidecar. Empty means base model.
	Weights string `json:"weights,omitempty"`
}

// CompleteResponse mirrors the Sting engine envelope.
type CompleteResponse struct {
	Type          string         `json:"type"`
	FunctionCalls []FunctionCall `json:"function_calls,omitempty"`
	Reasoning     string         `json:"reasoning,omitempty"`
	// Confidence is nil for tuned weights: fine-tuning does not update
	// the confidence head, so the engine reports no score. See gate.go.
	Confidence *float64 `json:"confidence,omitempty"`
	Error      string   `json:"error,omitempty"`
	ErrorCode  string   `json:"error_code,omitempty"`
	Escalate   bool     `json:"escalate,omitempty"`
}
