package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

type AnthropicProvider struct {
	client      *anthropic.Client
	tokenSource func() (string, error)
	apiBase     string
}

func NewAnthropicProvider(token string) *AnthropicProvider {
	client := anthropic.NewClient(option.WithAPIKey(token))
	return &AnthropicProvider{client: &client}
}

func NewAnthropicProviderWithBaseURL(token, apiBase string) *AnthropicProvider {
	client := anthropic.NewClient(option.WithAPIKey(token), option.WithBaseURL(apiBase))
	return &AnthropicProvider{client: &client, apiBase: apiBase}
}

func NewAnthropicProviderWithTokenSource(tokenSource func() (string, error)) *AnthropicProvider {
	client := anthropic.NewClient()
	return &AnthropicProvider{client: &client, tokenSource: tokenSource}
}

func NewAnthropicProviderWithTokenSourceAndBaseURL(tokenSource func() (string, error), apiBase string) *AnthropicProvider {
	client := anthropic.NewClient(option.WithBaseURL(apiBase))
	return &AnthropicProvider{client: &client, tokenSource: tokenSource, apiBase: apiBase}
}

func (p *AnthropicProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	opts := []option.RequestOption{}
	if p.tokenSource != nil {
		tok, err := p.tokenSource()
		if err != nil {
			return nil, err
		}
		opts = append(opts, option.WithAuthToken(tok))
	}
	if p.apiBase != "" {
		opts = append(opts, option.WithBaseURL(p.apiBase))
	}

	maxTokens := int64(8192)
	if val, ok := options["max_tokens"]; ok {
		switch v := val.(type) {
		case int:
			maxTokens = int64(v)
		case int64:
			maxTokens = v
		case float64:
			maxTokens = int64(v)
		}
	}

	params, err := buildAnthropicParams(messages, tools, model, maxTokens, options)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Messages.New(ctx, params, opts...)
	if err != nil {
		return nil, err
	}
	return parseAnthropicResponse(resp), nil
}

func (p *AnthropicProvider) StreamChat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onChunk func(string)) (*LLMResponse, error) {
	opts := []option.RequestOption{}
	if p.tokenSource != nil {
		tok, err := p.tokenSource()
		if err != nil {
			return nil, err
		}
		opts = append(opts, option.WithAuthToken(tok))
	}
	if p.apiBase != "" {
		opts = append(opts, option.WithBaseURL(p.apiBase))
	}

	maxTokens := int64(8192)
	if val, ok := options["max_tokens"]; ok {
		switch v := val.(type) {
		case int:
			maxTokens = int64(v)
		case int64:
			maxTokens = v
		case float64:
			maxTokens = int64(v)
		}
	}

	params, err := buildAnthropicParams(messages, tools, model, maxTokens, options)
	if err != nil {
		return nil, err
	}

	stream := p.client.Messages.NewStreaming(ctx, params, opts...)
	defer stream.Close()

	var msg anthropic.Message
	for stream.Next() {
		event := stream.Current()
		if err := msg.Accumulate(event); err != nil {
			return nil, err
		}

		// Extract chunk for streaming
		switch event.Type {
		case anthropic.MessageStreamEventTypeContentBlockDelta:
			if event.Delta.Type == anthropic.ContentBlockDeltaTypeInputJSONDelta {
				// Tool use JSON delta - we don't send this to UI
			} else if event.Delta.Text != "" {
				onChunk(event.Delta.Text)
			}
		}
	}

	if err := stream.Err(); err != nil {
		return nil, err
	}

	return parseAnthropicResponse(&msg), nil
}

func (p *AnthropicProvider) GetDefaultModel() string {
	return "claude-3-5-sonnet-20240620"
}

func buildAnthropicMessages(messages []Message) ([]anthropic.TextBlockParam, []anthropic.MessageParam) {
	var systemPrompts []anthropic.TextBlockParam
	out := make([]anthropic.MessageParam, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			block := anthropic.TextBlockParam{Text: anthropic.String(msg.Content)}
			if msg.CacheControl != nil && msg.CacheControl.Type == "ephemeral" {
				block.CacheControl = anthropic.NewCacheControlEphemeralParam()
			}
			systemPrompts = append(systemPrompts, block)
		case "user":
			out = append(out, anthropic.NewUserMessage(convertContentBlocks(msg)...))
		case "assistant":
			out = append(out, anthropic.NewAssistantMessage(convertContentBlocks(msg)...))
		case "tool":
			out = append(out, anthropic.NewUserMessage(anthropic.NewToolResultBlock(msg.ToolCallID, msg.Content, false)))
		}
	}
	return systemPrompts, out
}

func buildAnthropicParams(messages []Message, tools []ToolDefinition, model string, maxTokens int64, options map[string]interface{}) (anthropic.MessageNewParams, error) {
	systemPrompts, anthropicMessages := buildAnthropicMessages(messages)
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: maxTokens,
		Messages:  anthropicMessages,
	}

	if len(systemPrompts) > 0 {
		params.System = systemPrompts
	}

	if temp, ok := options["temperature"].(float64); ok {
		params.Temperature = anthropic.Float(temp)
	}

	// Handle Thinking/Reasoning
	if level, ok := options["thinking_level"].(string); ok && level != "" && level != "off" {
		applyThinkingConfig(&params, level)
	}

	if len(tools) > 0 {
		toolParams := make([]anthropic.ToolUnionParam, 0, len(tools))
		for _, t := range tools {
			if t.Type != "function" {
				continue
			}
			schema := anthropic.ToolInputSchemaParam{Type: constant.Object("").Default()}
			if props, ok := t.Function.Parameters["properties"]; ok {
				schema.Properties = props
			}
			if req, ok := t.Function.Parameters["required"]; ok {
				switch v := req.(type) {
				case []string:
					schema.Required = v
				case []interface{}:
					var out []string
					for _, r := range v {
						if s, ok := r.(string); ok {
							out = append(out, s)
						}
					}
					schema.Required = out
				}
			}
			toolParams = append(toolParams, anthropic.ToolUnionParamOfTool(schema, t.Function.Name))
		}
		if len(toolParams) > 0 {
			params.Tools = toolParams
		}
	}
	return params, nil
}

func convertContentBlocks(msg Message) []anthropic.ContentBlockParamUnion {
	if len(msg.MultiContent) == 0 {
		return []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(msg.Content)}
	}
	var blocks []anthropic.ContentBlockParamUnion
	for _, part := range msg.MultiContent {
		switch part.Type {
		case "text":
			blocks = append(blocks, anthropic.NewTextBlock(part.Text))
		case "image_url":
			if part.ImageURL == nil {
				continue
			}
			mime, data, ok := parseDataURL(part.ImageURL.URL)
			if !ok {
				continue
			}
			blocks = append(blocks, anthropic.NewImageBlockBase64(mime, data))
		}
	}
	if len(blocks) == 0 {
		blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
	}
	return blocks
}

func parseDataURL(url string) (string, string, bool) {
	if !strings.HasPrefix(url, "data:") {
		return "", "", false
	}
	parts := strings.SplitN(url, ",", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	meta := strings.TrimPrefix(parts[0], "data:")
	mime := strings.TrimSuffix(meta, ";base64")
	data := parts[1]
	if _, err := base64.StdEncoding.DecodeString(data); err != nil {
		return "", "", false
	}
	return mime, data, true
}

func parseAnthropicResponse(resp *anthropic.Message) *LLMResponse {
	var content string
	var reasoning string
	var toolCalls []ToolCall
	for _, block := range resp.Content {
		switch block.Type {
		case "thinking":
			reasoning += block.Thinking
		case "text":
			content += block.Text
		case "tool_use":
			var args map[string]interface{}
			if len(block.Input) > 0 {
				_ = json.Unmarshal(block.Input, &args)
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: args,
			})
		}
	}
	usage := &UsageInfo{
		PromptTokens:     int(resp.Usage.InputTokens),
		CompletionTokens: int(resp.Usage.OutputTokens),
		TotalTokens:      int(resp.Usage.InputTokens + resp.Usage.OutputTokens),
	}
	finishReason := "stop"
	switch resp.StopReason {
	case anthropic.StopReasonMaxTokens:
		finishReason = "length"
	case anthropic.StopReasonToolUse:
		finishReason = "tool_calls"
	case anthropic.StopReasonEndTurn:
		finishReason = "stop"
	}
	return &LLMResponse{
		Content:          content,
		ReasoningContent: reasoning,
		ToolCalls:        toolCalls,
		FinishReason:     finishReason,
		Usage:            usage,
	}
}

func applyThinkingConfig(params *anthropic.MessageNewParams, level string) {
	// Anthropic API rejects requests with temperature set alongside thinking.
	params.Temperature = anthropic.MessageNewParams{}.Temperature

	if level == "adaptive" {
		adaptive := anthropic.NewThinkingConfigAdaptiveParam()
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive}
		params.OutputConfig = anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffortHigh,
		}
		return
	}

	budget := int64(levelToBudget(level))
	if budget <= 0 {
		return
	}

	if budget >= params.MaxTokens {
		budget = params.MaxTokens - 1
	}
	params.Thinking = anthropic.ThinkingConfigParamOfEnabled(budget)
}

func levelToBudget(level string) int {
	switch level {
	case "low":
		return 4096
	case "medium":
		return 16384
	case "high":
		return 32000
	case "xhigh":
		return 64000
	default:
		return 0
	}
}

func buildClaudeParams(messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (anthropic.MessageNewParams, error) {
	maxTokens := int64(8192)
	if val, ok := options["max_tokens"]; ok {
		switch v := val.(type) {
		case int:
			maxTokens = int64(v)
		case int64:
			maxTokens = v
		case float64:
			maxTokens = int64(v)
		}
	}
	return buildAnthropicParams(messages, tools, model, maxTokens, options)
}

func parseClaudeResponse(resp *anthropic.Message) *LLMResponse {
	return parseAnthropicResponse(resp)
}
