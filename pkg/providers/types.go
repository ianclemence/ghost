package providers

import (
	"context"
	"encoding/json"
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
	// CacheReadTokens / CacheWriteTokens are the prompt tokens served
	// from, or written to, the provider's prompt cache (both already
	// counted in PromptTokens). Zero when the provider has no cache or
	// does not report it.
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	// CostUSD is measured cost when the provider reports it.
	// CostUnknown means no measured cost and no estimate applied:
	// unknown is never zero.
	CostUSD     float64 `json:"cost_usd,omitempty"`
	CostUnknown bool    `json:"cost_unknown,omitempty"`
}

// UnmarshalJSON reads the usage block of any OpenAI-compatible provider and
// normalizes prompt-cache hits into CacheReadTokens: DeepSeek reports
// "prompt_cache_hit_tokens", OpenAI "prompt_tokens_details.cached_tokens".
// Ghost's own serialized form ("cache_read_tokens") round-trips unchanged.
func (u *UsageInfo) UnmarshalJSON(b []byte) error {
	type plain UsageInfo
	var raw struct {
		plain
		DeepSeekCacheHit int `json:"prompt_cache_hit_tokens"`
		Details          *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*u = UsageInfo(raw.plain)
	if u.CacheReadTokens == 0 {
		switch {
		case raw.DeepSeekCacheHit > 0:
			u.CacheReadTokens = raw.DeepSeekCacheHit
		case raw.Details != nil && raw.Details.CachedTokens > 0:
			u.CacheReadTokens = raw.Details.CachedTokens
		}
	}
	return nil
}

type Message struct {
	Role             string        `json:"role"`
	Content          string        `json:"content"`
	MultiContent     []ContentPart `json:"multi_content,omitempty"`
	ReasoningContent string        `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID       string        `json:"tool_call_id,omitempty"`
	// ToolError marks a tool result as a failure, so providers with a
	// native error flag (Anthropic's is_error) tell the model the call
	// did not succeed instead of presenting the error text as output.
	ToolError    bool          `json:"tool_error,omitempty"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
	// SourceChannel records which surface a message came in on (mobile,
	// cli, telegram, voice, …). It is provenance, not conversation
	// identity: every surface shares one conversation, and this field only
	// notes where a turn originated. Empty for messages Ghost produced
	// without an external surface.
	SourceChannel string `json:"source_channel,omitempty"`
	// Kind marks a message Ghost started itself rather than wrote in reply:
	// "reminder" (something you asked to be told), "notice" (something Ghost
	// thought you should know) or "alert" (something that needs you). Empty for
	// ordinary conversation. Surfaces use it to set these apart.
	Kind string `json:"kind,omitempty"`
	// NoticeKey names the condition an alert or notice reports ("storage-critical"),
	// so the message can be marked resolved when the condition clears.
	NoticeKey string `json:"notice_key,omitempty"`
	// CreatedAt is when the message was persisted (zero when unknown).
	// Context building date-stamps loaded history with it so the model
	// can tell when "today"/"tomorrow" was actually said — a bare
	// relative word in old context is otherwise undatable. It is local
	// provenance only and is never serialized to providers.
	CreatedAt time.Time `json:"-"`
	// Interrupted marks a reply a restart cut off mid-sentence, which the
	// runtime put back into the transcript so nothing the model said was
	// lost. Surfaces show it as cut short instead of as an answer that
	// simply stopped. Local provenance only, never sent to providers.
	Interrupted bool `json:"-"`
}

// SystemPromptCacheBoundary separates the stable part of a system prompt
// (identity, behavior, skills — identical across turns) from per-turn state
// (current time, session, summary). Providers with explicit prompt caching
// place their cache breakpoint here; the volatile tail must never sit inside
// the cached prefix or the cache is rewritten on every turn.
const SystemPromptCacheBoundary = "<!-- SYSTEM_PROMPT_CACHE_BOUNDARY -->"

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
