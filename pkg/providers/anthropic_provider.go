package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	apiBase = normalizeAnthropicBase(apiBase)
	client := anthropic.NewClient(option.WithAPIKey(token), option.WithBaseURL(apiBase))
	return &AnthropicProvider{client: &client, apiBase: apiBase}
}

// NewAnthropicAPIKeyProvider is the native Messages API provider for an API
// key: prompt caching, native tool_use, refusal and cache-usage reporting —
// none of which the OpenAI-compatibility endpoint carries. Retries are kept
// to one so a dead provider hands over to Ghost's fallback chain quickly
// instead of spending the turn in SDK backoff.
func NewAnthropicAPIKeyProvider(apiKey, apiBase, proxy string) (*AnthropicProvider, error) {
	opts := []option.RequestOption{option.WithAPIKey(apiKey), option.WithMaxRetries(1)}
	apiBase = normalizeAnthropicBase(apiBase)
	if apiBase != "" {
		opts = append(opts, option.WithBaseURL(apiBase))
	}
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid anthropic proxy URL: %w", err)
		}
		opts = append(opts, option.WithHTTPClient(&http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u)}}))
	}
	client := anthropic.NewClient(opts...)
	return &AnthropicProvider{client: &client}, nil
}

// normalizeAnthropicBase turns a configured API base into the SDK's base
// URL. Ghost's config has always stored "https://api.anthropic.com/v1" (the
// shape the OpenAI-compatible path needed); the SDK appends "v1/messages"
// itself, so a trailing /v1 would double it.
func normalizeAnthropicBase(apiBase string) string {
	b := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	b = strings.TrimSuffix(b, "/v1")
	if b == "" {
		return ""
	}
	return b + "/"
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
		switch string(event.Type) {
		case "content_block_delta":
			if event.Delta.Type == "text_delta" {
				onChunk(event.Delta.Text)
			}
		}
	}

	if err := stream.Err(); err != nil {
		return nil, err
	}

	return parseAnthropicResponse(&msg), nil
}

// DefaultAnthropicModel is used only when no model is configured.
const DefaultAnthropicModel = "claude-opus-5-5"

func (p *AnthropicProvider) GetDefaultModel() string {
	return DefaultAnthropicModel
}

// buildAnthropicMessages renders Ghost's provider-neutral transcript in the
// Messages API shape. Three rules the API enforces (each a 400 otherwise):
// an assistant turn that called tools must carry those tool_use blocks, every
// tool_result must answer a tool_use in the turn before it, and text blocks
// must be non-empty. Parallel tool results are gathered into one user turn —
// splitting them across turns also teaches the model to stop calling tools in
// parallel.
//
// Thinking blocks are deliberately not replayed. Ghost rebuilds the system
// prompt each turn (memory, time, capability state), and models with
// preserved thinking reject a replayed block whose prefix has changed; the
// API documents stripping as the valid alternative.
func buildAnthropicMessages(messages []Message) ([]anthropic.TextBlockParam, []anthropic.MessageParam) {
	var systemPrompts []anthropic.TextBlockParam
	out := make([]anthropic.MessageParam, 0, len(messages))
	lastWasToolResults := false
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			systemPrompts = append(systemPrompts, systemBlocks(msg)...)
			continue
		case "user":
			if blocks := convertContentBlocks(msg); len(blocks) > 0 {
				out = append(out, anthropic.NewUserMessage(blocks...))
			}
		case "assistant":
			blocks := convertContentBlocks(msg)
			for _, tc := range msg.ToolCalls {
				name, input := toolCallNameAndInput(tc)
				if tc.ID == "" || name == "" {
					continue
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, input, name))
			}
			if len(blocks) > 0 {
				out = append(out, anthropic.NewAssistantMessage(blocks...))
			}
		case "tool":
			content := msg.Content
			if strings.TrimSpace(content) == "" {
				// An empty result is valid for the API but reads as
				// "nothing happened"; say so explicitly.
				content = "(no output)"
			}
			result := anthropic.NewToolResultBlock(msg.ToolCallID, content, msg.ToolError)
			if lastWasToolResults && len(out) > 0 {
				out[len(out)-1].Content = append(out[len(out)-1].Content, result)
			} else {
				out = append(out, anthropic.NewUserMessage(result))
			}
			lastWasToolResults = true
			continue
		}
		lastWasToolResults = false
	}
	return systemPrompts, out
}

// systemBlocks renders one system message. A cacheable message is split at
// SystemPromptCacheBoundary: the stable part carries the cache breakpoint
// (caching tools + stable system across turns), the per-turn tail follows
// uncached. Marking the whole message — current time included — made the
// cached prefix change every minute, so each call paid a cache write and
// almost never read one.
func systemBlocks(msg Message) []anthropic.TextBlockParam {
	stable, volatile := msg.Content, ""
	if i := strings.Index(stable, SystemPromptCacheBoundary); i >= 0 {
		stable, volatile = stable[:i], stable[i+len(SystemPromptCacheBoundary):]
	}
	stable, volatile = strings.TrimSpace(stable), strings.TrimSpace(volatile)
	var out []anthropic.TextBlockParam
	if stable != "" {
		block := anthropic.TextBlockParam{Text: stable}
		if msg.CacheControl != nil && msg.CacheControl.Type == "ephemeral" {
			block.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		out = append(out, block)
	}
	if volatile != "" {
		out = append(out, anthropic.TextBlockParam{Text: volatile})
	}
	return out
}

// toolCallNameAndInput reads a tool call in either of Ghost's two shapes
// (flat Name/Arguments, or OpenAI-style Function with a JSON string).
// The input is always a JSON object: the API rejects anything else.
func toolCallNameAndInput(tc ToolCall) (string, map[string]interface{}) {
	name := tc.Name
	args := tc.Arguments
	if tc.Function != nil {
		if name == "" {
			name = tc.Function.Name
		}
		if args == nil && strings.TrimSpace(tc.Function.Arguments) != "" {
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		}
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	return name, args
}

func buildAnthropicParams(messages []Message, tools []ToolDefinition, model string, maxTokens int64, options map[string]interface{}) (anthropic.MessageNewParams, error) {
	traits := anthropicTraitsFor(model)
	systemPrompts, anthropicMessages := buildAnthropicMessages(messages)
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: maxTokens,
		Messages:  anthropicMessages,
	}

	if len(systemPrompts) > 0 {
		params.System = systemPrompts
	}

	// Sampling parameters are rejected (400) from Opus 4.7 on and across
	// the 5.x line; there they are dropped rather than sent.
	if temp, ok := options["temperature"].(float64); ok && traits.acceptsSampling {
		params.Temperature = anthropic.Float(temp)
	}

	if level, ok := options["thinking_level"].(string); ok && level != "" && level != "off" {
		applyThinkingConfig(&params, model, level)
	} else {
		applyThinkingOff(&params, traits)
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
			tool := anthropic.ToolParam{Name: t.Function.Name, InputSchema: schema}
			if d := strings.TrimSpace(t.Function.Description); d != "" {
				tool.Description = anthropic.String(d)
			}
			toolParams = append(toolParams, anthropic.ToolUnionParam{OfTool: &tool})
		}
		if len(toolParams) > 0 {
			params.Tools = toolParams
		}
	}
	return params, nil
}

// convertContentBlocks returns the message's text and image blocks. Empty
// text is omitted (the API rejects empty text blocks), so an assistant turn
// that only called tools yields no blocks here.
func convertContentBlocks(msg Message) []anthropic.ContentBlockParamUnion {
	if len(msg.MultiContent) == 0 {
		if strings.TrimSpace(msg.Content) == "" {
			return nil
		}
		return []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(msg.Content)}
	}
	var blocks []anthropic.ContentBlockParamUnion
	for _, part := range msg.MultiContent {
		switch part.Type {
		case "text":
			if strings.TrimSpace(part.Text) == "" {
				continue
			}
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
	if len(blocks) == 0 && strings.TrimSpace(msg.Content) != "" {
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
	// input_tokens counts only the uncached remainder; the prompt the
	// model actually read is that plus cache reads and cache writes.
	prompt := resp.Usage.InputTokens + resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens
	usage := &UsageInfo{
		PromptTokens:     int(prompt),
		CompletionTokens: int(resp.Usage.OutputTokens),
		TotalTokens:      int(prompt + resp.Usage.OutputTokens),
		CacheReadTokens:  int(resp.Usage.CacheReadInputTokens),
		CacheWriteTokens: int(resp.Usage.CacheCreationInputTokens),
	}
	finishReason := "stop"
	switch resp.StopReason {
	case anthropic.StopReasonMaxTokens:
		finishReason = "length"
	case anthropic.StopReasonToolUse:
		finishReason = "tool_calls"
	case anthropic.StopReasonEndTurn:
		finishReason = "stop"
	case anthropic.StopReasonRefusal:
		// A declined request carries no usable answer. Whatever partial
		// text streamed before the decline is discarded so it can never
		// be presented as the reply; the empty response lets the
		// fallback chain try the next candidate.
		finishReason = "refusal"
		content = ""
		toolCalls = nil
	}
	return &LLMResponse{
		Content:          stripInlineReasoning(content),
		ReasoningContent: reasoning,
		ToolCalls:        toolCalls,
		FinishReason:     finishReason,
		Usage:            usage,
	}
}

// anthropicTraits are the request-shape rules one model enforces. Every
// rule here exists because breaking it is a 400, not a preference.
type anthropicTraits struct {
	major, minor int
	known        bool
	// adaptive: thinking is {type:"adaptive"} + output_config.effort
	// (4.6+); older models take enabled+budget_tokens.
	adaptive bool
	// acceptsSampling: temperature/top_p are accepted (removed from
	// Opus 4.7 on, and on every 5.x model).
	acceptsSampling bool
	// offMode is how "thinking off" is expressed:
	//   "absent"   — omit the param; the server default is off (≤4.8)
	//   "disabled" — explicit {type:"disabled"} (Opus 5 / Sonnet 5, whose
	//                server default is adaptive-on)
	//   "low"      — thinking cannot be disabled (Opus 5.5, Sonnet 5.5,
	//                Fable, Mythos, and newer); omit it and ask for low
	//                effort, the documented way to keep a turn quick
	offMode string
	// xhigh: the xhigh effort level exists (4.7+).
	xhigh bool
}

// anthropicTraitsFor derives the rules from the model id. Unknown ids get
// the forward-looking shape of current models (adaptive, no sampling,
// thinking left to the server) — omitting a parameter never 400s.
func anthropicTraitsFor(model string) anthropicTraits {
	major, minor, ok := anthropicModelGeneration(model)
	m := strings.ToLower(model)
	t := anthropicTraits{major: major, minor: minor, known: ok}
	alwaysOnFamily := strings.Contains(m, "fable") || strings.Contains(m, "mythos")
	switch {
	case !ok:
		t.adaptive, t.offMode = true, "absent"
	case alwaysOnFamily:
		t.adaptive, t.offMode, t.xhigh = true, "low", true
	case major < 4 || (major == 4 && minor < 6):
		t.acceptsSampling, t.offMode = true, "absent"
	case major == 4 && minor == 6:
		t.adaptive, t.acceptsSampling, t.offMode = true, true, "absent"
	case major == 4:
		t.adaptive, t.offMode, t.xhigh = true, "absent", true
	case major == 5 && minor == 0:
		t.adaptive, t.offMode, t.xhigh = true, "disabled", true
	default:
		t.adaptive, t.offMode, t.xhigh = true, "low", true
	}
	return t
}

// applyThinkingOff expresses Ghost's default "no deliberate thinking" in the
// form the model accepts.
func applyThinkingOff(params *anthropic.MessageNewParams, t anthropicTraits) {
	switch t.offMode {
	case "disabled":
		disabled := anthropic.NewThinkingConfigDisabledParam()
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfDisabled: &disabled}
	case "low":
		params.OutputConfig = anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffortLow}
	}
}

func applyThinkingConfig(params *anthropic.MessageNewParams, model, level string) {
	t := anthropicTraitsFor(model)
	// Anthropic API rejects requests with temperature set alongside thinking.
	params.Temperature = anthropic.MessageNewParams{}.Temperature

	// enabled+budget_tokens is deprecated on 4.6 and rejected on 4.7+;
	// those models take adaptive thinking with an effort instead.
	if level == "adaptive" || t.adaptive {
		adaptive := anthropic.NewThinkingConfigAdaptiveParam()
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive}
		params.OutputConfig = anthropic.OutputConfigParam{
			Effort: anthropicEffortFor(t, level),
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

// anthropicModelGeneration parses the generation from a Claude model id:
// "claude-opus-4-6" -> 4,6; "claude-sonnet-5" -> 5,0; "claude-3-5-sonnet"
// -> 3,5. Date snapshots ("-20251001", "@20251001") and provider prefixes
// ("anthropic/") are ignored, so a dated Haiku 4.5 is 4.5, not 5.x.
func anthropicModelGeneration(model string) (major, minor int, ok bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndexAny(m, "/:"); i >= 0 {
		m = m[i+1:]
	}
	var nums []int
	for _, tok := range strings.FieldsFunc(m, func(r rune) bool {
		return r == '-' || r == '.' || r == '_' || r == '@'
	}) {
		if len(tok) == 0 || len(tok) > 2 {
			continue // family words and 8-digit date snapshots
		}
		n := 0
		digits := true
		for _, r := range tok {
			if r < '0' || r > '9' {
				digits = false
				break
			}
			n = n*10 + int(r-'0')
		}
		if digits {
			nums = append(nums, n)
		}
	}
	switch len(nums) {
	case 0:
		return 0, 0, false
	case 1:
		return nums[0], 0, true
	default:
		return nums[0], nums[1], true
	}
}

// anthropicAdaptiveEffort maps a thinking level onto the adaptive output
// effort for a current model.
func anthropicAdaptiveEffort(level string) anthropic.OutputConfigEffort {
	return anthropicEffortFor(anthropicTraits{xhigh: true}, level)
}

// anthropicEffortFor maps a thinking level onto an effort the model accepts
// (xhigh exists from 4.7 on; earlier adaptive models step up to max).
func anthropicEffortFor(t anthropicTraits, level string) anthropic.OutputConfigEffort {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "low", "none", "off":
		return anthropic.OutputConfigEffortLow
	case "medium":
		return anthropic.OutputConfigEffortMedium
	case "xhigh":
		if t.xhigh {
			return anthropic.OutputConfigEffort("xhigh")
		}
		return anthropic.OutputConfigEffortMax
	case "max":
		return anthropic.OutputConfigEffortMax
	default:
		return anthropic.OutputConfigEffortHigh
	}
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
