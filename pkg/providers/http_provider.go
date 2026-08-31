// Ghost - Ultra-lightweight personal AI agent
// Inspired by and based on GHOST: https://github.com/ianclemence/ghost
// License: MIT
//
// Copyright (c) 2026 Ghost contributors

package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/auth"
	"github.com/ianclemence/ghost/pkg/config"
)

type HTTPProvider struct {
	apiKey         string
	apiBase        string
	httpClient     *http.Client
	embeddingModel string
	defaultModel   string
}

func NewHTTPProvider(apiKey, apiBase, proxy, embeddingModel string) *HTTPProvider {
	client := &http.Client{
		Timeout: 300 * time.Second, // Increased to 5 minutes for slow model loading on Pi
	}

	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err == nil {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxyURL),
			}
		}
	}

	return &HTTPProvider{
		apiKey:         apiKey,
		apiBase:        strings.TrimRight(apiBase, "/"),
		httpClient:     client,
		embeddingModel: embeddingModel,
	}
}

// SetDefaultModel sets the default model for this provider.
func (p *HTTPProvider) SetDefaultModel(model string) {
	p.defaultModel = model
}

func (p *HTTPProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	return p.StreamChat(ctx, messages, tools, model, options, nil)
}

func (p *HTTPProvider) StreamChat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onChunk func(string)) (*LLMResponse, error) {
	if p.apiBase == "" {
		return nil, fmt.Errorf("API base not configured")
	}

	// Strip provider prefix from model name (e.g., moonshot/kimi-k2.5 -> kimi-k2.5,
	// or deepseek:deepseek-v4-flash -> deepseek-v4-flash).
	idx := strings.IndexAny(model, "/:")
	if idx != -1 {
		prefix := model[:idx]
		switch prefix {
		case "moonshot", "kimi", "nvidia", "ollama", "anthropic", "openai", "google", "groq", "deepseek", "openrouter", "copilot":
			model = model[idx+1:]
		}
	}

	requestBody := map[string]interface{}{
		"model":    model,
		"messages": toOpenAIMessages(messages),
	}

	if len(tools) > 0 {
		requestBody["tools"] = tools
		requestBody["tool_choice"] = "auto"
	}

	if maxTokens, ok := options["max_tokens"].(int); ok {
		lowerModel := strings.ToLower(model)
		if strings.Contains(lowerModel, "glm") || strings.Contains(lowerModel, "o1") {
			requestBody["max_completion_tokens"] = maxTokens
		} else {
			requestBody["max_tokens"] = maxTokens
		}
	}

	if temperature, ok := options["temperature"].(float64); ok {
		lowerModel := strings.ToLower(model)
		// Kimi k2 models only support temperature=1
		if strings.Contains(lowerModel, "kimi") && strings.Contains(lowerModel, "k2") {
			requestBody["temperature"] = 1.0
		} else {
			requestBody["temperature"] = temperature
		}
	}

	if thinking, ok := options["thinking"].(bool); ok {
		requestBody["thinking"] = thinking
	}

	// Map generic thinking options to Ollama's expected "think" parameter
	// - For Qwen/DeepSeek: bool true/false
	// - For GPT-OSS: "low" | "medium" | "high"
	var thinkParam interface{} = nil
	if v, ok := options["thinking"].(bool); ok {
		thinkParam = v
	} else if lvl, ok := options["thinking_level"].(string); ok {
		switch strings.ToLower(lvl) {
		case "low", "medium", "high":
			thinkParam = lvl
		case "on", "enabled", "true":
			thinkParam = true
		default:
			thinkParam = false
		}
	} else {
		// Default: disable thinking to improve latency on local models
		thinkParam = false
	}

	useNative := (strings.Contains(p.apiBase, "11434") || strings.Contains(p.apiBase, "ollama.com")) && !strings.Contains(p.apiBase, "/v1")
	if useNative {
		// Build a minimal payload for Ollama's native chat API. Ollama's
		// /api/chat takes media as a top-level "images" array of base64
		// strings, not an OpenAI content block array.
		type nativeMsg struct {
			Role    string   `json:"role"`
			Content string   `json:"content"`
			Images  []string `json:"images,omitempty"`
		}
		nativeMsgs := make([]nativeMsg, 0, len(messages))
		for _, m := range messages {
			content := m.Content
			var images []string
			if content == "" && len(m.MultiContent) > 0 {
				var sb strings.Builder
				for _, part := range m.MultiContent {
					switch {
					case part.Type == "text" && part.Text != "":
						sb.WriteString(part.Text)
					case part.ImageURL != nil && strings.HasPrefix(part.ImageURL.URL, "data:"):
						// data:<mime>;base64,<payload>
						if b64 := imageBase64Part(part.ImageURL.URL); b64 != "" {
							images = append(images, b64)
						}
					}
				}
				content = sb.String()
			}
			nativeMsgs = append(nativeMsgs, nativeMsg{Role: m.Role, Content: content, Images: images})
		}
		nativeBody := map[string]interface{}{
			"model":    model,
			"messages": nativeMsgs,
			// The native /api/chat endpoint streams NDJSON by default, but
			// we read the whole body and decode a single JSON object below,
			// so request a non-streaming response.
			"stream": false,
		}
		if thinkParam != nil {
			nativeBody["think"] = thinkParam
		}
		// Pass Ollama-native options (temperature, num_predict)
		ollamaOpts := map[string]interface{}{}
		if temp, ok := options["temperature"].(float64); ok {
			ollamaOpts["temperature"] = temp
		}
		if maxTok, ok := options["max_tokens"].(int); ok {
			ollamaOpts["num_predict"] = maxTok
		}
		if len(ollamaOpts) > 0 {
			nativeBody["options"] = ollamaOpts
		}
		jsonData, err := json.Marshal(nativeBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		root := strings.TrimRight(p.apiBase, "/")
		url := root + "/api/chat"
		if strings.HasSuffix(root, "/api") {
			url = root + "/chat"
		}
		if strings.HasSuffix(root, "/api/chat") {
			url = root
		}
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if p.apiKey != "" && p.apiKey != "ollama" {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		}
		resp, err := p.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to send request: %w", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API request failed:\n  Status: %d\n  Body:   %s", resp.StatusCode, string(body))
		}
		return p.parseNativeResponse(body)
	} else {
		jsonData, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+"/chat/completions", bytes.NewReader(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if p.apiKey != "" && p.apiKey != "ollama" {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		}
		resp, err := p.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to send request: %w", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API request failed:\n  Status: %d\n  Body:   %s", resp.StatusCode, string(body))
		}
		return p.parseResponse(body)
	}
}

func (p *HTTPProvider) parseNativeResponse(body []byte) (*LLMResponse, error) {
	var apiResponse struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function *struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		Usage *UsageInfo `json:"usage"`
	}
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	toolCalls := make([]ToolCall, 0, len(apiResponse.Message.ToolCalls))
	for _, tc := range apiResponse.Message.ToolCalls {
		arguments := make(map[string]interface{})
		name := ""
		if tc.Type == "function" && tc.Function != nil {
			name = tc.Function.Name
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &arguments); err != nil {
					arguments["raw"] = tc.Function.Arguments
				}
			}
		} else if tc.Function != nil {
			name = tc.Function.Name
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &arguments); err != nil {
					arguments["raw"] = tc.Function.Arguments
				}
			}
		}
		toolCalls = append(toolCalls, ToolCall{
			ID:        tc.ID,
			Name:      name,
			Arguments: arguments,
		})
	}
	return &LLMResponse{
		Content:          apiResponse.Message.Content,
		ReasoningContent: apiResponse.Message.ReasoningContent,
		ToolCalls:        toolCalls,
		FinishReason:     "stop",
		Usage:            apiResponse.Usage,
	}, nil
}
func (p *HTTPProvider) parseResponse(body []byte) (*LLMResponse, error) {
	var apiResponse struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function *struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *UsageInfo `json:"usage"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(apiResponse.Choices) == 0 {
		return &LLMResponse{
			Content:      "",
			FinishReason: "stop",
		}, nil
	}

	choice := apiResponse.Choices[0]

	toolCalls := make([]ToolCall, 0, len(choice.Message.ToolCalls))
	for _, tc := range choice.Message.ToolCalls {
		arguments := make(map[string]interface{})
		name := ""

		// Handle OpenAI format with nested function object
		if tc.Type == "function" && tc.Function != nil {
			name = tc.Function.Name
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &arguments); err != nil {
					arguments["raw"] = tc.Function.Arguments
				}
			}
		} else if tc.Function != nil {
			// Legacy format without type field
			name = tc.Function.Name
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &arguments); err != nil {
					arguments["raw"] = tc.Function.Arguments
				}
			}
		}

		toolCalls = append(toolCalls, ToolCall{
			ID:        tc.ID,
			Name:      name,
			Arguments: arguments,
		})
	}

	return &LLMResponse{
		Content:          choice.Message.Content,
		ReasoningContent: choice.Message.ReasoningContent,
		ToolCalls:        toolCalls,
		FinishReason:     choice.FinishReason,
		Usage:            apiResponse.Usage,
	}, nil
}

func (p *HTTPProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if p.apiBase == "" {
		return nil, fmt.Errorf("API base not configured")
	}

	model := p.embeddingModel
	if model == "" {
		model = "text-embedding-3-small"
	}

	requestBody := map[string]interface{}{
		"model": model,
		"input": text,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	useNative := strings.Contains(p.apiBase, "11434") || strings.Contains(p.apiBase, "ollama.com")
	if useNative {
		root := strings.TrimRight(p.apiBase, "/")
		url := root + "/api/embed"
		if strings.HasSuffix(root, "/api") {
			url = root + "/embed"
		}
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if p.apiKey != "" && p.apiKey != "ollama" {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		}
		resp, err := p.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to send request: %w", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API request failed:\n  Status: %d\n  Body:   %s", resp.StatusCode, string(body))
		}
		var native struct {
			Embedding  []float32   `json:"embedding"`
			Embeddings [][]float32 `json:"embeddings"`
		}
		if err := json.Unmarshal(body, &native); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}
		if len(native.Embeddings) > 0 {
			return native.Embeddings[0], nil
		}
		if len(native.Embedding) > 0 {
			return native.Embedding, nil
		}
		return nil, fmt.Errorf("no embedding returned")
	} else {
		req, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+"/embeddings", bytes.NewReader(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if p.apiKey != "" && p.apiKey != "ollama" {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		}
		resp, err := p.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to send request: %w", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API request failed:\n  Status: %d\n  Body:   %s", resp.StatusCode, string(body))
		}
		var apiResponse struct {
			Data []struct {
				Embedding []float32 `json:"embedding"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &apiResponse); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}
		if len(apiResponse.Data) == 0 {
			return nil, fmt.Errorf("no embedding returned")
		}
		return apiResponse.Data[0].Embedding, nil
	}
}

func (p *HTTPProvider) GetDefaultModel() string {
	return p.defaultModel
}

func createClaudeAuthProvider() (LLMProvider, error) {
	cred, err := auth.GetCredential("anthropic")
	if err != nil {
		return nil, fmt.Errorf("loading auth credentials: %w", err)
	}
	if cred == nil {
		return nil, fmt.Errorf("no credentials for anthropic. Run: GHOST auth login --provider anthropic")
	}
	return NewClaudeProviderWithTokenSource(cred.AccessToken, createClaudeTokenSource()), nil
}

func createCodexAuthProvider() (LLMProvider, error) {
	cred, err := auth.GetCredential("openai")
	if err != nil {
		return nil, fmt.Errorf("loading auth credentials: %w", err)
	}
	if cred == nil {
		return nil, fmt.Errorf("no credentials for openai. Run: GHOST auth login --provider openai")
	}
	return NewCodexProviderWithTokenSource(cred.AccessToken, cred.AccountID, createCodexTokenSource()), nil
}

func CreateProvider(cfg *config.Config) (LLMProvider, error) {
	model := cfg.Agents.Defaults.Model
	providerName := strings.ToLower(cfg.Agents.Defaults.Provider)

	var apiKey, apiBase, proxy string

	lowerModel := strings.ToLower(model)

	// First, try to use explicitly configured provider
	if providerName != "" {
		switch providerName {
		case "moonshot", "kimi":
			if cfg.Providers.Moonshot.APIKey != "" {
				apiKey = cfg.Providers.Moonshot.APIKey
				apiBase = cfg.Providers.Moonshot.APIBase
				return NewMoonshotProvider(apiKey, apiBase), nil
			}
		case "groq":
			if cfg.Providers.Groq.APIKey != "" {
				apiKey = cfg.Providers.Groq.APIKey
				apiBase = cfg.Providers.Groq.APIBase
				if apiBase == "" {
					apiBase = "https://api.groq.com/openai/v1"
				}
			}
		case "openai", "gpt":
			if cfg.Providers.OpenAI.APIKey != "" || cfg.Providers.OpenAI.AuthMethod != "" {
				if cfg.Providers.OpenAI.AuthMethod == "oauth" || cfg.Providers.OpenAI.AuthMethod == "token" {
					return createCodexAuthProvider()
				}
				apiKey = cfg.Providers.OpenAI.APIKey
				apiBase = cfg.Providers.OpenAI.APIBase
				if apiBase == "" {
					apiBase = "https://api.openai.com/v1"
				}
			}
		case "anthropic", "claude":
			if cfg.Providers.Anthropic.APIKey != "" || cfg.Providers.Anthropic.AuthMethod != "" {
				if cfg.Providers.Anthropic.AuthMethod == "oauth" || cfg.Providers.Anthropic.AuthMethod == "token" {
					return createClaudeAuthProvider()
				}
				apiKey = cfg.Providers.Anthropic.APIKey
				apiBase = cfg.Providers.Anthropic.APIBase
				if apiBase == "" {
					apiBase = "https://api.anthropic.com/v1"
				}
			}
		case "openrouter":
			if cfg.Providers.OpenRouter.APIKey != "" {
				apiKey = cfg.Providers.OpenRouter.APIKey
				if cfg.Providers.OpenRouter.APIBase != "" {
					apiBase = cfg.Providers.OpenRouter.APIBase
				} else {
					apiBase = "https://openrouter.ai/api/v1"
				}
			}
		case "zhipu", "glm":
			if cfg.Providers.Zhipu.APIKey != "" {
				apiKey = cfg.Providers.Zhipu.APIKey
				apiBase = cfg.Providers.Zhipu.APIBase
				if apiBase == "" {
					apiBase = "https://open.bigmodel.cn/api/paas/v4"
				}
			}
		case "gemini", "google":
			if cfg.Providers.Gemini.APIKey != "" {
				apiKey = cfg.Providers.Gemini.APIKey
				apiBase = cfg.Providers.Gemini.APIBase
				if apiBase == "" {
					apiBase = "https://generativelanguage.googleapis.com/v1beta/openai"
				}
			}
		case "vllm":
			if cfg.Providers.VLLM.APIBase != "" {
				apiKey = cfg.Providers.VLLM.APIKey
				apiBase = cfg.Providers.VLLM.APIBase
			}
		case "shengsuanyun":
			if cfg.Providers.ShengSuanYun.APIKey != "" {
				apiKey = cfg.Providers.ShengSuanYun.APIKey
				apiBase = cfg.Providers.ShengSuanYun.APIBase
				if apiBase == "" {
					apiBase = "https://router.shengsuanyun.com/api/v1"
				}
			}
		case "claude-cli", "claudecode", "claude-code":
			workspace := cfg.Agents.Defaults.Workspace
			if workspace == "" {
				workspace = "."
			}
			return NewClaudeCliProvider(workspace), nil
		case "deepseek":
			if cfg.Providers.DeepSeek.APIKey != "" {
				apiKey = cfg.Providers.DeepSeek.APIKey
				apiBase = cfg.Providers.DeepSeek.APIBase
				if apiBase == "" {
					apiBase = "https://api.deepseek.com"
				}
			}
		case "github_copilot", "copilot":
			if cfg.Providers.GitHubCopilot.APIBase != "" {
				apiBase = cfg.Providers.GitHubCopilot.APIBase
			} else {
				apiBase = "localhost:4321"
			}
			return NewGitHubCopilotProvider(apiBase, cfg.Providers.GitHubCopilot.ConnectMode, model)

		case "ollama":
			apiKey = cfg.Providers.Ollama.APIKey
			apiBase = cfg.Providers.Ollama.APIBase
			if apiBase == "" {
				apiBase = "http://localhost:11434"
			}
		}

	}

	// Fallback: detect provider from model name
	if apiKey == "" && apiBase == "" {
		switch {
		case strings.HasPrefix(model, "ollama/"):
			apiKey = cfg.Providers.Ollama.APIKey
			apiBase = cfg.Providers.Ollama.APIBase
			if apiBase == "" {
				apiBase = "http://localhost:11434"
			}
			model = model[7:] // Strip "ollama/" prefix
		case (strings.Contains(lowerModel, "kimi") || strings.Contains(lowerModel, "moonshot") || strings.HasPrefix(model, "moonshot/")) && cfg.Providers.Moonshot.APIKey != "":
			apiKey = cfg.Providers.Moonshot.APIKey
			apiBase = cfg.Providers.Moonshot.APIBase
			return NewMoonshotProvider(apiKey, apiBase), nil

		case strings.HasPrefix(model, "openrouter/") || strings.HasPrefix(model, "anthropic/") || strings.HasPrefix(model, "openai/") || strings.HasPrefix(model, "meta-llama/") || strings.HasPrefix(model, "deepseek/") || strings.HasPrefix(model, "google/"):
			apiKey = cfg.Providers.OpenRouter.APIKey
			proxy = cfg.Providers.OpenRouter.Proxy
			if cfg.Providers.OpenRouter.APIBase != "" {
				apiBase = cfg.Providers.OpenRouter.APIBase
			} else {
				apiBase = "https://openrouter.ai/api/v1"
			}

		case (strings.Contains(lowerModel, "claude") || strings.HasPrefix(model, "anthropic/")) && (cfg.Providers.Anthropic.APIKey != "" || cfg.Providers.Anthropic.AuthMethod != ""):
			if cfg.Providers.Anthropic.AuthMethod == "oauth" || cfg.Providers.Anthropic.AuthMethod == "token" {
				return createClaudeAuthProvider()
			}
			apiKey = cfg.Providers.Anthropic.APIKey
			apiBase = cfg.Providers.Anthropic.APIBase
			proxy = cfg.Providers.Anthropic.Proxy
			if apiBase == "" {
				apiBase = "https://api.anthropic.com/v1"
			}

		case (strings.Contains(lowerModel, "gpt") || strings.HasPrefix(model, "openai/")) && (cfg.Providers.OpenAI.APIKey != "" || cfg.Providers.OpenAI.AuthMethod != ""):
			if cfg.Providers.OpenAI.AuthMethod == "oauth" || cfg.Providers.OpenAI.AuthMethod == "token" {
				return createCodexAuthProvider()
			}
			apiKey = cfg.Providers.OpenAI.APIKey
			apiBase = cfg.Providers.OpenAI.APIBase
			proxy = cfg.Providers.OpenAI.Proxy
			if apiBase == "" {
				apiBase = "https://api.openai.com/v1"
			}

		case (strings.Contains(lowerModel, "gemini") || strings.HasPrefix(model, "google/")) && cfg.Providers.Gemini.APIKey != "":
			apiKey = cfg.Providers.Gemini.APIKey
			apiBase = cfg.Providers.Gemini.APIBase
			proxy = cfg.Providers.Gemini.Proxy
			if apiBase == "" {
				apiBase = "https://generativelanguage.googleapis.com/v1beta/openai"
			}

		case (strings.Contains(lowerModel, "glm") || strings.Contains(lowerModel, "zhipu") || strings.Contains(lowerModel, "zai")) && cfg.Providers.Zhipu.APIKey != "":
			apiKey = cfg.Providers.Zhipu.APIKey
			apiBase = cfg.Providers.Zhipu.APIBase
			proxy = cfg.Providers.Zhipu.Proxy
			if apiBase == "" {
				apiBase = "https://open.bigmodel.cn/api/paas/v4"
			}

		case (strings.Contains(lowerModel, "groq") || strings.HasPrefix(model, "groq/")) && cfg.Providers.Groq.APIKey != "":
			apiKey = cfg.Providers.Groq.APIKey
			apiBase = cfg.Providers.Groq.APIBase
			proxy = cfg.Providers.Groq.Proxy
			if apiBase == "" {
				apiBase = "https://api.groq.com/openai/v1"
			}

		case (strings.Contains(lowerModel, "nvidia") || strings.HasPrefix(model, "nvidia/")) && cfg.Providers.Nvidia.APIKey != "":
			apiKey = cfg.Providers.Nvidia.APIKey
			apiBase = cfg.Providers.Nvidia.APIBase
			proxy = cfg.Providers.Nvidia.Proxy
			if apiBase == "" {
				apiBase = "https://integrate.api.nvidia.com/v1"
			}

		case cfg.Providers.VLLM.APIBase != "":
			apiKey = cfg.Providers.VLLM.APIKey
			apiBase = cfg.Providers.VLLM.APIBase
			proxy = cfg.Providers.VLLM.Proxy

		default:
			if cfg.Providers.OpenRouter.APIKey != "" {
				apiKey = cfg.Providers.OpenRouter.APIKey
				proxy = cfg.Providers.OpenRouter.Proxy
				if cfg.Providers.OpenRouter.APIBase != "" {
					apiBase = cfg.Providers.OpenRouter.APIBase
				} else {
					apiBase = "https://openrouter.ai/api/v1"
				}
			} else {
				return nil, fmt.Errorf("no API key configured for model: %s", model)
			}
		}
	}

	if apiKey == "" && !strings.HasPrefix(model, "bedrock/") && !strings.Contains(apiBase, "localhost") && !strings.Contains(apiBase, "11434") {
		return nil, fmt.Errorf("no API key configured for provider (model: %s)", model)
	}

	if apiBase == "" {
		return nil, fmt.Errorf("no API base configured for provider (model: %s)", model)
	}

	p := NewHTTPProvider(apiKey, apiBase, proxy, cfg.Agents.Defaults.EmbeddingModel)
	p.SetDefaultModel(model)
	return p, nil
}

// toOpenAIMessages renders messages in the OpenAI-compatible Chat Completions
// shape. When a message carries visual parts (images/video), its `content`
// becomes an array of content blocks ([{type,text}, {type,image_url,
// image_url:{url}}]) instead of a plain string — the format DeepSeek, OpenAI
// and other compatible providers expect for vision. Non-visual messages pass
// through unchanged so tool-call/tool-result serialization is untouched.
func toOpenAIMessages(messages []Message) []interface{} {
	out := make([]interface{}, 0, len(messages))
	for _, m := range messages {
		if blk := visualContentBlocks(m.MultiContent); len(blk) > 0 {
			mm := map[string]interface{}{
				"role":    m.Role,
				"content": blk,
			}
			if m.ToolCalls != nil {
				mm["tool_calls"] = m.ToolCalls
			}
			if m.ToolCallID != "" {
				mm["tool_call_id"] = m.ToolCallID
			}
			out = append(out, mm)
			continue
		}
		// Non-visual messages keep their existing shape (content string +
		// structured fields) so tool calls serialize exactly as before.
		out = append(out, m)
	}
	return out
}

func visualContentBlocks(parts []ContentPart) []map[string]interface{} {
	blk := []map[string]interface{}{}
	for _, p := range parts {
		switch {
		case p.Type == "text" && p.Text != "":
			blk = append(blk, map[string]interface{}{"type": "text", "text": p.Text})
		case p.ImageURL != nil && p.ImageURL.URL != "":
			blk = append(blk, map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": p.ImageURL.URL}})
		case p.VideoURL != nil && p.VideoURL.URL != "":
			blk = append(blk, map[string]interface{}{"type": "video_url", "video_url": map[string]interface{}{"url": p.VideoURL.URL}})
		}
	}
	return blk
}

// imageBase64Part extracts the base64 payload from a data:<mime>;base64,<b64> URL.
func imageBase64Part(dataURL string) string {
	if i := strings.Index(dataURL, ";base64,"); i >= 0 {
		return dataURL[i+len(";base64,"):]
	}
	return ""
}
