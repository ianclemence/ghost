package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sipeed/picoclaw/pkg/logger"
)

type KimiProvider struct {
	apiKey     string
	apiBase    string
	httpClient *http.Client
}

func NewKimiProvider(apiKey, apiBase string) *KimiProvider {
	if apiBase == "" {
		apiBase = "https://api.moonshot.ai/v1"
	}
	return &KimiProvider{
		apiKey:  apiKey,
		apiBase: apiBase,
		httpClient: &http.Client{
			Timeout: 180 * time.Second, // Long timeout for thinking
		},
	}
}

type kimiRequest struct {
	Model       string                 `json:"model"`
	Messages    []kimiMessage          `json:"messages"`
	Thinking    *kimiThinking          `json:"thinking,omitempty"`
	Tools       []ToolDefinition       `json:"tools,omitempty"`
	ToolChoice  interface{}            `json:"tool_choice,omitempty"` // "auto" or "none"
	Temperature float64                `json:"temperature,omitempty"`
	MaxTokens   int                    `json:"max_tokens,omitempty"`
	Stream      bool                   `json:"stream"`
}

type kimiThinking struct {
	Type string `json:"type"` // "enabled" or "disabled"
}

type kimiMessage struct {
	Role             string            `json:"role"`
	Content          interface{}       `json:"content"` // string or []kimiContentPart
	ToolCalls        []ToolCall        `json:"tool_calls,omitempty"`
	ToolCallID       string            `json:"tool_call_id,omitempty"`
}

type kimiContentPart struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	ImageURL *kimiURL    `json:"image_url,omitempty"`
	VideoURL *kimiURL    `json:"video_url,omitempty"`
}

type kimiURL struct {
	URL string `json:"url"`
}

type kimiResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Index        int         `json:"index"`
		Message      kimiMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage *UsageInfo `json:"usage"`
}

func (p *KimiProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	// Build request
	reqBody := kimiRequest{
		Model:    model,
		Messages: make([]kimiMessage, 0, len(messages)),
		Stream:   false,
	}

	if len(tools) > 0 {
		reqBody.Tools = tools
		reqBody.ToolChoice = "auto"
	}

	// Handle Thinking
	if thinking, ok := options["thinking"].(bool); ok && thinking {
		reqBody.Thinking = &kimiThinking{Type: "enabled"}
		// Kimi docs say: tool_choice can only be "auto" or "none" with thinking enabled.
		// Temp/TopP fixed values handled by Kimi, we don't send them if thinking is enabled to avoid errors.
	} else {
		// Non-thinking mode parameters
		if temp, ok := options["temperature"].(float64); ok {
			reqBody.Temperature = temp
		}
		if maxTokens, ok := options["max_tokens"].(int); ok {
			reqBody.MaxTokens = maxTokens
		}
	}
    
    // Default to disabled if not specified, unless model implies it? 
    // Docs say default is enabled for thinking models. But we want manual control.
    // If user didn't specify thinking option, we might want to default to disabled to be safe with tools?
    // User requirement: "Add a command... to enable Kimi K2.5's Thinking Mode... It is currently disabled or set to auto."
    // So we assume default is disabled unless requested.
    if reqBody.Thinking == nil {
         reqBody.Thinking = &kimiThinking{Type: "disabled"}
    }

	for _, msg := range messages {
		kMsg := kimiMessage{
			Role:       msg.Role,
			ToolCalls:  msg.ToolCalls,
			ToolCallID: msg.ToolCallID,
		}

		if len(msg.MultiContent) > 0 {
			parts := make([]kimiContentPart, 0, len(msg.MultiContent))
			for _, part := range msg.MultiContent {
				kp := kimiContentPart{
					Type: part.Type,
					Text: part.Text,
				}
				if part.ImageURL != nil {
					kp.ImageURL = &kimiURL{URL: part.ImageURL.URL}
				}
				if part.VideoURL != nil {
					kp.VideoURL = &kimiURL{URL: part.VideoURL.URL}
				}
				parts = append(parts, kp)
			}
			kMsg.Content = parts
		} else {
			kMsg.Content = msg.Content
		}
		reqBody.Messages = append(reqBody.Messages, kMsg)
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+"/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	logger.DebugCF("kimi", "Sending request", map[string]interface{}{
		"model":    model,
		"thinking": reqBody.Thinking,
		"tools":    len(tools),
	})

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var kResp kimiResponse
	if err := json.Unmarshal(body, &kResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(kResp.Choices) == 0 {
		return nil, fmt.Errorf("empty choices in response")
	}

	choice := kResp.Choices[0]

	// Process tool calls to ensure format compatibility
	for i := range choice.Message.ToolCalls {
		tc := &choice.Message.ToolCalls[i]

		// If Function is present, extract Name and Arguments
		if tc.Function != nil {
			// Set top-level Name
			tc.Name = tc.Function.Name

			// Parse arguments string to map
			if tc.Function.Arguments != "" {
				var args map[string]interface{}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err == nil {
					tc.Arguments = args
				} else {
					logger.ErrorCF("kimi", "Failed to parse tool arguments", map[string]interface{}{
						"tool":  tc.Name,
						"args":  tc.Function.Arguments,
						"error": err.Error(),
					})
				}
			}
		}
	}

	// Extract content string if it's mixed
	contentStr := ""
    // Kimi returns string content usually
    if str, ok := choice.Message.Content.(string); ok {
        contentStr = str
    } else {
        // Should not happen for assistant response usually, but handle just in case
        jsonContent, _ := json.Marshal(choice.Message.Content)
        contentStr = string(jsonContent)
    }

	return &LLMResponse{
		Content:      contentStr,
		ToolCalls:    choice.Message.ToolCalls,
		FinishReason: choice.FinishReason,
		Usage:        kResp.Usage,
	}, nil
}

func (p *KimiProvider) GetDefaultModel() string {
	return "kimi-k2.5"
}
