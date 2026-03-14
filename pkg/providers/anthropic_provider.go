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

	systemPrompt, anthropicMessages := buildAnthropicMessages(messages)
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: maxTokens,
		Messages:  anthropicMessages,
	}

	if systemPrompt != "" {
		params.System = []anthropic.TextBlockParam{
			{Text: systemPrompt},
		}
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

	resp, err := p.client.Messages.New(ctx, params, opts...)
	if err != nil {
		return nil, err
	}
	return parseAnthropicResponse(resp), nil
}

func (p *AnthropicProvider) GetDefaultModel() string {
	return "claude-3-5-sonnet-20240620"
}

func buildAnthropicMessages(messages []Message) (string, []anthropic.MessageParam) {
	var systemPrompt string
	out := make([]anthropic.MessageParam, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			if systemPrompt == "" {
				systemPrompt = msg.Content
			}
		case "user":
			out = append(out, anthropic.NewUserMessage(convertContentBlocks(msg)...))
		case "assistant":
			out = append(out, anthropic.NewAssistantMessage(convertContentBlocks(msg)...))
		case "tool":
			out = append(out, anthropic.NewUserMessage(anthropic.NewToolResultBlock(msg.ToolCallID, msg.Content, false)))
		}
	}
	return systemPrompt, out
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
	var toolCalls []ToolCall
	for _, block := range resp.Content {
		switch block.Type {
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
	return &LLMResponse{
		Content:   content,
		ToolCalls: toolCalls,
		Usage:     usage,
	}
}
