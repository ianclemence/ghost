// Package ollamaruntime adapts the existing Ollama/OpenAI-compatible
// HTTP provider stack behind the infer.InferenceRuntime contract, so the
// agent reasons about capabilities instead of "is Ollama running?".
package ollamaruntime

import (
	"context"
	"strings"

	"github.com/ianclemence/ghost/pkg/infer"
	"github.com/ianclemence/ghost/pkg/providers"
)

// Runtime wraps an existing providers.LLMProvider as an InferenceRuntime.
type Runtime struct {
	provider providers.LLMProvider
	embed    providers.EmbeddingProvider
	model    string
}

// New wraps provider (usually *providers.HTTPProvider) with its default model.
func New(p providers.LLMProvider, model string) *Runtime {
	var e providers.EmbeddingProvider
	if ep, ok := p.(providers.EmbeddingProvider); ok {
		e = ep
	}
	if model == "" && p != nil {
		model = p.GetDefaultModel()
	}
	return &Runtime{provider: p, embed: e, model: model}
}

func (r *Runtime) Name() string { return "ollama" }

func (r *Runtime) Locality() infer.Locality { return infer.LocalityPod }

func (r *Runtime) Models(ctx context.Context) ([]infer.ModelRef, error) {
	cap := providers.Describe(r.model)
	return []infer.ModelRef{{
		ID:         r.model,
		Runtime:    "ollama",
		Locality:   infer.LocalityPod,
		ContextMax: cap.MaxContext,
	}}, nil
}

func (r *Runtime) Capabilities(ctx context.Context, modelID string) ([]infer.Capability, error) {
	cap := providers.Describe(modelID)
	out := []infer.Capability{infer.CapChat}
	if cap.ToolCalling {
		out = append(out, infer.CapToolCalling)
	}
	if cap.Vision {
		out = append(out, infer.CapVision)
	}
	if cap.Reasoning {
		out = append(out, infer.CapStructuredOutput)
	} else {
		// Conservative: structured output assumed available via JSON mode on
		// modern local chat models; strict per-model data lives in registry.
		out = append(out, infer.CapStructuredOutput)
	}
	return out, nil
}

func (r *Runtime) Satisfies(ctx context.Context, modelID string, req infer.Requirements) (bool, string) {
	if req.LocalOnly && false { // pod-local counts as local; cloud never reaches here
		return false, "local-only violated"
	}
	caps, err := r.Capabilities(ctx, modelID)
	if err != nil {
		return false, err.Error()
	}
	if !infer.SatisfiesAll(caps, req.Capabilities) {
		return false, "missing capability"
	}
	if req.MinContext > 0 {
		if c := providers.Describe(modelID); c.MaxContext < req.MinContext {
			return false, "context too small"
		}
	}
	return true, ""
}

func toProviderMessages(in []infer.Message) []providers.Message {
	out := make([]providers.Message, 0, len(in))
	for _, m := range in {
		pm := providers.Message{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			pm.ToolCalls = append(pm.ToolCalls, providers.ToolCall{
				ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments,
			})
		}
		out = append(out, pm)
	}
	return out
}

func toProviderTools(in []infer.ToolSchema) []providers.ToolDefinition {
	out := make([]providers.ToolDefinition, 0, len(in))
	for _, t := range in {
		out = append(out, providers.ToolDefinition{
			Type: "function",
			Function: providers.ToolFunctionDefinition{
				Name: t.Name, Description: t.Description, Parameters: t.Parameters,
			},
		})
	}
	return out
}

func fromProviderResponse(res *providers.LLMResponse) *infer.GenerateResult {
	if res == nil {
		return &infer.GenerateResult{FinishReason: "empty"}
	}
	r := &infer.GenerateResult{
		Content: res.Content, FinishReason: res.FinishReason,
	}
	if res.Usage != nil {
		r.PromptTokens = res.Usage.PromptTokens
		r.CompletionTokens = res.Usage.CompletionTokens
	}
	for _, tc := range res.ToolCalls {
		name := tc.Name
		if tc.Function != nil {
			name = tc.Function.Name
		}
		r.ToolCalls = append(r.ToolCalls, infer.ToolCall{ID: tc.ID, Name: name, RawArgs: func() string {
			if tc.Function != nil {
				return tc.Function.Arguments
			}
			return ""
		}()})
	}
	return r
}

func (r *Runtime) Generate(ctx context.Context, req infer.GenerateRequest) (*infer.GenerateResult, error) {
	model := req.Model.ID
	if model == "" {
		model = r.model
	}
	// Strip legacy "ollama/" prefix: capability identity, not provider path.
	model = strings.TrimPrefix(model, "ollama/")
	res, err := r.provider.Chat(ctx, toProviderMessages(req.Messages), toProviderTools(req.Tools), model, req.Options)
	if err != nil {
		return nil, err
	}
	return fromProviderResponse(res), nil
}

func (r *Runtime) Stream(ctx context.Context, req infer.GenerateRequest, onToken func(string)) (*infer.GenerateResult, error) {
	model := req.Model.ID
	if model == "" {
		model = r.model
	}
	model = strings.TrimPrefix(model, "ollama/")
	if sp, ok := r.provider.(providers.StreamingProvider); ok {
		res, err := sp.StreamChat(ctx, toProviderMessages(req.Messages), toProviderTools(req.Tools), model, req.Options, onToken)
		if err != nil {
			return nil, err
		}
		return fromProviderResponse(res), nil
	}
	res, err := r.provider.Chat(ctx, toProviderMessages(req.Messages), toProviderTools(req.Tools), model, req.Options)
	if err != nil {
		return nil, err
	}
	out := fromProviderResponse(res)
	if onToken != nil && out.Content != "" {
		onToken(out.Content)
	}
	return out, nil
}

func (r *Runtime) Embed(ctx context.Context, modelID, text string) ([]float32, error) {
	if r.embed == nil {
		return nil, errNoEmbed()
	}
	return r.embed.Embed(ctx, text)
}

func (r *Runtime) Health(ctx context.Context) infer.Health {
	if r.provider == nil {
		return infer.Health{Available: false, Reason: "no provider bound"}
	}
	return infer.Health{Available: true, LoadedModel: r.model}
}
