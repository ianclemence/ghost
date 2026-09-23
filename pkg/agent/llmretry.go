package agent

import (
	"context"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/provider"
	"github.com/ianclemence/ghost/pkg/providers"
)

// llmRetryDelays bounds turn-level retries: one slow or blipped provider
// call must not fail the whole turn, but retries stay few and short so a
// genuinely dead provider still fails fast and honestly.
var llmRetryDelays = []time.Duration{2 * time.Second, 4 * time.Second}

// callLLMWithRetry runs fn, retrying transient-class failures with backoff.
// The class comes from the shared provider taxonomy, so the turn loop
// retries exactly what the capability layer would. Non-retryable classes
// (auth, config, validation, cancellation) return immediately. Context
// cancellation is honored between attempts.
func callLLMWithRetry(ctx context.Context, iteration int, fn func() (*providers.LLMResponse, error)) (*providers.LLMResponse, error) {
	return provider.DoWithRetry(ctx, llmRetryDelays, func(attempt int, class provider.FailureClass) {
		logger.WarnCF("agent", "LLM call transient failure, retrying",
			map[string]interface{}{"iteration": iteration, "attempt": attempt, "class": string(class)})
	}, fn)
}
