package providers

import (
	"context"
	"fmt"

	"github.com/ianclemence/ghost/pkg/auth"
)

type ClaudeProvider struct {
	delegate *AnthropicProvider
}

func NewClaudeProvider(token string) *ClaudeProvider {
	return &ClaudeProvider{
		delegate: NewAnthropicProvider(token),
	}
}

func NewClaudeProviderWithBaseURL(token, apiBase string) *ClaudeProvider {
	return &ClaudeProvider{
		delegate: NewAnthropicProviderWithBaseURL(token, apiBase),
	}
}

func NewClaudeProviderWithTokenSource(token string, tokenSource func() (string, error)) *ClaudeProvider {
	return &ClaudeProvider{
		delegate: NewAnthropicProviderWithTokenSource(tokenSource),
	}
}

func NewClaudeProviderWithTokenSourceAndBaseURL(
	token string, tokenSource func() (string, error), apiBase string,
) *ClaudeProvider {
	return &ClaudeProvider{
		delegate: NewAnthropicProviderWithTokenSourceAndBaseURL(tokenSource, apiBase),
	}
}

func newClaudeProviderWithDelegate(delegate *AnthropicProvider) *ClaudeProvider {
	return &ClaudeProvider{delegate: delegate}
}

func (p *ClaudeProvider) Chat(
	ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]any,
) (*LLMResponse, error) {
	resp, err := p.delegate.Chat(ctx, messages, tools, model, options)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (p *ClaudeProvider) GetDefaultModel() string {
	return p.delegate.GetDefaultModel()
}

func createClaudeTokenSource() func() (string, error) {
	return func() (string, error) {
		cred, err := auth.GetCredential("anthropic")
		if err != nil {
			return "", fmt.Errorf("loading auth credentials: %w", err)
		}
		if cred == nil {
			return "", fmt.Errorf("no credentials for anthropic. Run: GHOST auth login --provider anthropic")
		}
		return cred.AccessToken, nil
	}
}
