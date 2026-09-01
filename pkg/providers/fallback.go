package providers

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
)

type FallbackCandidate struct {
	Name     string
	Provider LLMProvider
	Model    string
}

type CooldownTracker struct {
	mu       sync.Mutex
	cooldown time.Duration
	until    map[string]time.Time
}

func NewCooldownTracker(cooldown time.Duration) *CooldownTracker {
	return &CooldownTracker{
		cooldown: cooldown,
		until:    make(map[string]time.Time),
	}
}

func (c *CooldownTracker) IsAvailable(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.until[name]; ok && time.Now().Before(t) {
		return false
	}
	return true
}

func (c *CooldownTracker) MarkFailure(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.until[name] = time.Now().Add(c.cooldown)
}

func (c *CooldownTracker) MarkSuccess(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.until, name)
}

type FallbackChain struct {
	tracker *CooldownTracker
}

func NewFallbackChain(cooldown time.Duration) *FallbackChain {
	return &FallbackChain{tracker: NewCooldownTracker(cooldown)}
}

// usableResponse reports whether an LLM response actually produced something.
// A valid model turn either emits content or makes tool calls; a degenerate
// response with neither is treated as a failure so the chain tries the next
// candidate (this is what prevents a silent empty reply from a misconfigured
// or dead active model).
func usableResponse(resp *LLMResponse) bool {
	return resp != nil && (strings.TrimSpace(resp.Content) != "" || len(resp.ToolCalls) > 0)
}

func (f *FallbackChain) Execute(ctx context.Context, candidates []FallbackCandidate, run func(FallbackCandidate) (*LLMResponse, error)) (*LLMResponse, error) {
	var lastErr error
	for _, c := range candidates {
		if f.tracker != nil && !f.tracker.IsAvailable(c.Name) {
			logger.DebugCF("fallback", "skipping candidate (cooldown)", map[string]interface{}{"name": c.Name})
			continue
		}
		logger.InfoCF("fallback", "trying candidate", map[string]interface{}{"name": c.Name})
		resp, err := run(c)
		if err == nil && usableResponse(resp) {
			if f.tracker != nil {
				f.tracker.MarkSuccess(c.Name)
			}
			return resp, nil
		}
		if err != nil {
			logger.ErrorCF("fallback", "candidate failed", map[string]interface{}{"name": c.Name, "error": err.Error()})
		} else {
			// Empty/did-not-respond: treat as failure so we fall back instead of
			// surfacing a blank answer.
			err = fmt.Errorf("empty response from %s", c.Name)
			logger.WarnCF("fallback", "candidate returned empty response", map[string]interface{}{"name": c.Name})
		}
		if f.tracker != nil {
			f.tracker.MarkFailure(c.Name)
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no available providers")
}
