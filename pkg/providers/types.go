package providers

import (
	"context"
	"time"
)

type ToolCall struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type,omitempty"`
	Function  *FunctionCall          `json:"function,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type LLMResponse struct {
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	FinishReason     string     `json:"finish_reason"`
	Usage            *UsageInfo `json:"usage,omitempty"`
}

type UsageInfo struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// CostUSD is measured cost when the provider reports it.
	// CostUnknown means no measured cost and no estimate applied:
	// unknown is never zero.
	CostUSD     float64 `json:"cost_usd,omitempty"`
	CostUnknown bool    `json:"cost_unknown,omitempty"`
}

type Message struct {
	Role             string        `json:"role"`
	Content          string        `json:"content"`
	MultiContent     []ContentPart `json:"multi_content,omitempty"`
	ReasoningContent string        `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID       string        `json:"tool_call_id,omitempty"`
	CacheControl     *CacheControl `json:"cache_control,omitempty"`
	// SourceChannel records which surface a message came in on (mobile,
	// cli, telegram, voice, …). It is provenance, not conversation
	// identity: every surface shares one conversation, and this field only
	// notes where a turn originated. Empty for messages Ghost produced
	// without an external surface.
	SourceChannel string `json:"source_channel,omitempty"`
	// CreatedAt is when the message was persisted (zero when unknown).
	// Context building date-stamps loaded history with it so the model
	// can tell when "today"/"tomorrow" was actually said — a bare
	// relative word in old context is otherwise undatable. It is local
	// provenance only and is never serialized to providers.
	CreatedAt time.Time `json:"-"`
}

type CacheControl struct {
	Type string `json:"type"` // e.g., "ephemeral"
}

type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
	VideoURL *VideoURL `json:"video_url,omitempty"`
}

type ImageURL struct {
	URL string `json:"url"`
}

type VideoURL struct {
	URL string `json:"url"`
}

type LLMProvider interface {
	Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error)
	GetDefaultModel() string
}

type StreamingProvider interface {
	StreamChat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onChunk func(string)) (*LLMResponse, error)
}

type EmbeddingProvider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type FileUploader interface {
	UploadFile(ctx context.Context, filePath string, purpose string) (string, error)
}

type ToolDefinition struct {
	Type     string                 `json:"type"`
	Function ToolFunctionDefinition `json:"function"`
}

type ToolFunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}
