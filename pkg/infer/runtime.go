// Package infer defines the runtime-independent inference contract.
//
// Ghost asks for capabilities (language generation, tool calling, embeddings,
// vision, structured output, context capacity, locality), never "is Ollama
// running?". Ollama/cloud/mobile runtimes implement InferenceRuntime; the
// agent loop programs against this interface.
package infer

import "context"

// Capability is a single inference ability Ghost can require.
type Capability string

const (
	CapChat             Capability = "chat"
	CapToolCalling      Capability = "tool_calling"
	CapStructuredOutput Capability = "structured_output"
	CapEmbeddings       Capability = "embeddings"
	CapVision           Capability = "vision"
	CapSpeech           Capability = "speech"
)

// Locality describes where inference executes.
type Locality string

const (
	LocalityPhone Locality = "phone"
	LocalityPod   Locality = "pod"
	LocalityCloud Locality = "cloud"
)

// ModelRef identifies a concrete model on a runtime.
type ModelRef struct {
	ID         string   `json:"id"`
	Version    string   `json:"version,omitempty"`
	Runtime    string   `json:"runtime"`
	Locality   Locality `json:"locality"`
	ContextMax int      `json:"context_max,omitempty"`
}

// Requirements is what Ghost needs for one turn.
type Requirements struct {
	Capabilities []Capability `json:"capabilities"`
	MinContext   int          `json:"min_context,omitempty"`
	// LocalOnly forbids cloud execution (privacy mode).
	LocalOnly bool `json:"local_only,omitempty"`
	// AllowPod permits routing to the Pod runtime.
	AllowPod bool `json:"allow_pod,omitempty"`
	// MaxLatencyMs is advisory; runtimes report health, planner decides.
	MaxLatencyMs int `json:"max_latency_ms,omitempty"`
}

// Health reports runtime/model readiness.
type Health struct {
	Available   bool   `json:"available"`
	Reason      string `json:"reason,omitempty"`
	LoadedModel string `json:"loaded_model,omitempty"`
	LatencyMs   int64  `json:"latency_ms,omitempty"`
}

// GenerateRequest is one non-streaming generation.
type GenerateRequest struct {
	Model    ModelRef               `json:"model"`
	Messages []Message              `json:"messages"`
	Tools    []ToolSchema           `json:"tools,omitempty"`
	JSONMode bool                   `json:"json_mode,omitempty"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

// Message is runtime-neutral (mirrors providers.Message semantics without
// importing the provider package, avoiding an import cycle).
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is a model-requested invocation.
type ToolCall struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
	RawArgs   string                 `json:"raw_args,omitempty"`
}

// ToolSchema describes a callable tool.
type ToolSchema struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// GenerateResult is the completed generation.
type GenerateResult struct {
	Content          string     `json:"content"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	FinishReason     string     `json:"finish_reason,omitempty"`
	PromptTokens     int        `json:"prompt_tokens,omitempty"`
	CompletionTokens int        `json:"completion_tokens,omitempty"`
}

// InferenceRuntime is implemented by Ollama, mobile-local, cloud, and future
// runtimes without rewriting the agent.
type InferenceRuntime interface {
	// Name is the stable runtime id, e.g. "ollama", "mobile-local", "cloud".
	Name() string
	// Locality reports where this runtime executes.
	Locality() Locality
	// Models lists installed/available models.
	Models(ctx context.Context) ([]ModelRef, error)
	// Capabilities reports what the named model can do.
	Capabilities(ctx context.Context, modelID string) ([]Capability, error)
	// Satisfies reports whether the model meets requirements.
	Satisfies(ctx context.Context, modelID string, req Requirements) (bool, string)
	// Generate runs one completion.
	Generate(ctx context.Context, req GenerateRequest) (*GenerateResult, error)
	// Stream runs a completion, invoking onToken per delta. It must honor
	// ctx cancellation and stop consuming inference resources promptly.
	Stream(ctx context.Context, req GenerateRequest, onToken func(string)) (*GenerateResult, error)
	// Embed returns an embedding vector when CapEmbeddings is supported.
	Embed(ctx context.Context, modelID, text string) ([]float32, error)
	// Health reports liveness and loaded-model state.
	Health(ctx context.Context) Health
}

// HasCapability is a helper for requirement checks.
func HasCapability(have []Capability, want Capability) bool {
	for _, c := range have {
		if c == want {
			return true
		}
	}
	return false
}

// SatisfiesAll reports whether have covers every wanted capability.
func SatisfiesAll(have, want []Capability) bool {
	for _, w := range want {
		if !HasCapability(have, w) {
			return false
		}
	}
	return true
}
