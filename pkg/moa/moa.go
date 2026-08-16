// Package moa implements Mixture of Agents — a pattern where multiple
// advisor models run in parallel on the same prompt, then an aggregator
// synthesizes the best advice into a final response.
//
// Architecture:
//
//	User prompt -> [Advisor 1, Advisor 2, ..., Advisor N] (parallel)
//	                    |           |                   |
//	                    v           v                   v
//	              [AdvisorOutput, AdvisorOutput, ..., AdvisorOutput]
//	                         |
//	                         v
//	                  Aggregator model -> Final response
package moa

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/providers"
)

// AdvisorConfig describes one advisor slot in the MoA fan-out.
type AdvisorConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Label    string `json:"label,omitempty"` // optional human-readable label
}

// Config configures the MoA system.
type Config struct {
	Enabled          bool           `json:"enabled"`
	Advisors         []AdvisorConfig `json:"advisors"`
	AggregatorModel  string         `json:"aggregator_model"`
	AggregatorProvider string       `json:"aggregator_provider"`
	TimeoutSeconds   int            `json:"timeout_seconds"`   // per-advisor timeout
	MaxAdvisors      int            `json:"max_advisors"`      // cap on parallel advisors
	Temperature      float64        `json:"temperature"`
}

// DefaultConfig returns sensible MoA defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:          false,
		Advisors:         []AdvisorConfig{},
		AggregatorModel:  "",
		AggregatorProvider: "",
		TimeoutSeconds:   30,
		MaxAdvisors:      5,
		Temperature:      0.7,
	}
}

// AdvisorOutput holds the result from one advisor model.
type AdvisorOutput struct {
	Label    string
	Provider string
	Model    string
	Content  string
	Duration time.Duration
	Error    error
}

// Result holds the full MoA output.
type Result struct {
	AdvisorOutputs []AdvisorOutput
	Aggregated     string
	TotalDuration  time.Duration
}

// ReferenceSystemPrompt tells advisor models they are advisory only.
const ReferenceSystemPrompt = `You are a reference advisor in a Mixture of Agents process. You are NOT the acting agent and you DO NOT execute anything: you cannot call tools, run commands, browse, or access files, repositories, or URLs, and you should not try to or apologize for being unable to. A separate aggregator/orchestrator model holds those capabilities and will take the actual actions.

CRITICAL: You must NEVER claim or imply that you have executed a command, downloaded a file, accessed a URL, or performed any action. You can only analyze and advise based on the conversation context.

The conversation below is the current state of a task handled by that acting agent. Your job is to give your most intelligent analysis of that state: understand the goal, reason about the problem, and advise on what to do next. Surface the best approach, concrete next steps, likely pitfalls and risks, and anything the acting agent may have missed or gotten wrong.

Respond with your advice directly — no preamble, no disclaimers about tools or access. Your response is private guidance handed to the aggregator, not an answer shown to the user.`

// AggregatorSystemPrompt tells the aggregator how to synthesize advisor outputs.
const AggregatorSystemPrompt = `You are the aggregator in a Mixture of Agents process. You have received advice from multiple reference models. Your job is to synthesize the best advice into a single, coherent, actionable response.

Rules:
- Weigh advice by quality and relevance, not by which model produced it.
- Resolve contradictions by choosing the most logically sound position.
- If all advisors agree, confirm that consensus.
- If advisors disagree, explain the disagreement and justify your resolution.
- Provide a clear, direct response as if you were the sole agent answering the user.
- Do NOT mention the MoA process, advisors, or that multiple models were consulted.`

// ProviderResolver maps a provider name to an LLMProvider instance.
type ProviderResolver func(providerName string) (providers.LLMProvider, bool)

// MoA orchestrates the Mixture of Agents pattern.
type MoA struct {
	config   Config
	resolver ProviderResolver
	mu       sync.RWMutex
}

// New creates a new MoA instance.
func New(config Config, resolver ProviderResolver) *MoA {
	if config.TimeoutSeconds <= 0 {
		config.TimeoutSeconds = 30
	}
	if config.MaxAdvisors <= 0 {
		config.MaxAdvisors = 5
	}
	return &MoA{
		config:   config,
		resolver: resolver,
	}
}

// ShouldUseMoA determines whether MoA should be activated for this request.
// MoA activates when enabled and there are at least 2 advisors configured.
func (m *MoA) ShouldUseMoA() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.Enabled && len(m.config.Advisors) >= 2
}

// Run executes the full MoA pipeline: fan-out to advisors, then aggregate.
// The original user messages are passed to advisors (without tool definitions).
// The aggregator receives the original messages plus all advisor outputs.
func (m *MoA) Run(
	ctx context.Context,
	userMessages []providers.Message,
) (*Result, error) {
	start := time.Now()

	// Clamp advisors to max
	advisors := m.config.Advisors
	if len(advisors) > m.config.MaxAdvisors {
		advisors = advisors[:m.config.MaxAdvisors]
	}

	// Fan out to all advisors in parallel
	advisorOutputs := m.runAdvisorsParallel(ctx, advisors, userMessages)

	// Aggregate
	aggregated, err := m.aggregate(ctx, userMessages, advisorOutputs)
	if err != nil {
		return nil, fmt.Errorf("moa aggregation failed: %w", err)
	}

	return &Result{
		AdvisorOutputs: advisorOutputs,
		Aggregated:     aggregated,
		TotalDuration:  time.Since(start),
	}, nil
}

// runAdvisorsParallel fans out to all advisor models concurrently.
func (m *MoA) runAdvisorsParallel(
	ctx context.Context,
	advisors []AdvisorConfig,
	userMessages []providers.Message,
) []AdvisorOutput {
	type indexedOutput struct {
		index  int
		output AdvisorOutput
	}

	outputs := make([]AdvisorOutput, len(advisors))
	var wg sync.WaitGroup

	timeout := time.Duration(m.config.TimeoutSeconds) * time.Second

	for i, advisor := range advisors {
		wg.Add(1)
		go func(idx int, adv AdvisorConfig) {
			defer wg.Done()

			output := m.runOneAdvisor(ctx, adv, userMessages, timeout)
			outputs[idx] = output
		}(i, advisor)
	}

	wg.Wait()
	return outputs
}

// runOneAdvisor calls a single advisor model with a timeout.
func (m *MoA) runOneAdvisor(
	ctx context.Context,
	advisor AdvisorConfig,
	userMessages []providers.Message,
	timeout time.Duration,
) AdvisorOutput {
	start := time.Now()

	label := advisor.Label
	if label == "" {
		label = fmt.Sprintf("%s/%s", advisor.Provider, advisor.Model)
	}

	output := AdvisorOutput{
		Label:    label,
		Provider: advisor.Provider,
		Model:    advisor.Model,
	}

	// Resolve provider
	provider, ok := m.resolver(advisor.Provider)
	if !ok {
		output.Error = fmt.Errorf("provider %q not found", advisor.Provider)
		output.Duration = time.Since(start)
		return output
	}

	// Build advisor messages: system prompt + user conversation (no tools)
	messages := buildAdvisorMessages(userMessages)

	// Create timeout context
	advCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	options := map[string]interface{}{
		"temperature": m.config.Temperature,
	}

	resp, err := provider.Chat(advCtx, messages, nil, advisor.Model, options)
	if err != nil {
		output.Error = err
		output.Duration = time.Since(start)
		return output
	}

	output.Content = resp.Content
	output.Duration = time.Since(start)

	logger.DebugCF("moa", "Advisor completed", map[string]interface{}{
		"label":    label,
		"duration": output.Duration.Milliseconds(),
	})

	return output
}

// buildAdvisorMessages prepares messages for an advisor (advisory role).
func buildAdvisorMessages(userMessages []providers.Message) []providers.Message {
	var messages []providers.Message

	// Add advisory system prompt
	messages = append(messages, providers.Message{
		Role:    "system",
		Content: ReferenceSystemPrompt,
	})

	// Add the user conversation as context (skip tool calls/results for advisors)
	for _, msg := range userMessages {
		switch msg.Role {
		case "system":
			// Skip the agent's own system prompt — advisors don't need it
			continue
		case "tool":
			// Skip tool results — advisors can't call tools
			continue
		default:
			// Keep user and assistant messages
			messages = append(messages, providers.Message{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}

	// Ensure we end with a user message (required by many providers)
	if len(messages) == 0 || messages[len(messages)-1].Role != "user" {
		messages = append(messages, providers.Message{
			Role:    "user",
			Content: "[Analyze the above conversation and provide your advice.]",
		})
	}

	return messages
}

// aggregate sends advisor outputs to the aggregator model for synthesis.
func (m *MoA) aggregate(
	ctx context.Context,
	originalMessages []providers.Message,
	advisorOutputs []AdvisorOutput,
) (string, error) {
	provider, ok := m.resolver(m.config.AggregatorProvider)
	if !ok {
		return "", fmt.Errorf("aggregator provider %q not found", m.config.AggregatorProvider)
	}

	// Build the aggregator prompt
	messages := buildAggregatorMessages(originalMessages, advisorOutputs)

	options := map[string]interface{}{
		"temperature": m.config.Temperature,
	}

	resp, err := provider.Chat(ctx, messages, nil, m.config.AggregatorModel, options)
	if err != nil {
		return "", fmt.Errorf("aggregator call failed: %w", err)
	}

	return resp.Content, nil
}

// buildAggregatorMessages prepares the full prompt for the aggregator.
func buildAggregatorMessages(
	originalMessages []providers.Message,
	advisorOutputs []AdvisorOutput,
) []providers.Message {
	var messages []providers.Message

	// Aggregator system prompt
	messages = append(messages, providers.Message{
		Role:    "system",
		Content: AggregatorSystemPrompt,
	})

	// Build the advisor summary block
	var advisorBlock strings.Builder
	advisorBlock.WriteString("## Advisor Outputs\n\n")

	for i, output := range advisorOutputs {
		advisorBlock.WriteString(fmt.Sprintf("### Advisor %d: %s\n", i+1, output.Label))
		if output.Error != nil {
			advisorBlock.WriteString(fmt.Sprintf("**Error:** %s\n\n", output.Error.Error()))
		} else {
			advisorBlock.WriteString(fmt.Sprintf("**Model:** %s/%s\n", output.Provider, output.Model))
			advisorBlock.WriteString(fmt.Sprintf("**Response:**\n%s\n\n", output.Content))
		}
	}

	// Add the original conversation context (stripped of tools)
	var contextBlock strings.Builder
	contextBlock.WriteString("## Original Conversation Context\n\n")
	for _, msg := range originalMessages {
		if msg.Role == "system" || msg.Role == "tool" {
			continue
		}
		role := "User"
		if msg.Role == "assistant" {
			role = "Assistant"
		}
		contextBlock.WriteString(fmt.Sprintf("**%s:** %s\n\n", role, msg.Content))
	}

	// Final user request
	messages = append(messages, providers.Message{
		Role:    "user",
		Content: fmt.Sprintf("%s\n%s\n\nBased on the advisor outputs and conversation context above, provide your final aggregated response.", contextBlock.String(), advisorBlock.String()),
	})

	return messages
}

// FormatResult returns a human-readable summary of the MoA result.
func (r *Result) FormatResult() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("MoA completed in %v (%d advisors)\n", r.TotalDuration.Round(time.Millisecond), len(r.AdvisorOutputs)))

	for _, output := range r.AdvisorOutputs {
		status := "OK"
		if output.Error != nil {
			status = fmt.Sprintf("ERROR: %s", output.Error)
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s — %v\n", status, output.Label, output.Duration.Round(time.Millisecond)))
	}

	return sb.String()
}

// GetAdvisorLabels returns sorted advisor labels for display.
func (r *Result) GetAdvisorLabels() []string {
	labels := make([]string, 0, len(r.AdvisorOutputs))
	for _, o := range r.AdvisorOutputs {
		labels = append(labels, o.Label)
	}
	sort.Strings(labels)
	return labels
}
