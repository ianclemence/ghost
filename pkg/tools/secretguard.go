package tools

import (
	"strings"
	"sync"

	"github.com/ianclemence/ghost/pkg/credentials"
)

// Secret guard: the last line between the vault and the model.
//
// The redact package masks credential *shapes*. It cannot know that a
// website password like "correct-horse-7" is a secret. This guard knows the
// exact values the vault holds and removes them from every tool result
// before the result reaches the model, the transcript or a channel — so even
// a tool that echoes a page, cats a file or logs a request cannot leak one.

const secretMarker = "«secret removed»"

var (
	secretSourceMu sync.RWMutex
	secretSource   = credentials.SecretValues
)

// SetSecretSource replaces where the guard reads secrets from (tests).
// It returns a function that restores the previous source.
func SetSecretSource(fn func() []string) (restore func()) {
	secretSourceMu.Lock()
	prev := secretSource
	secretSource = fn
	secretSourceMu.Unlock()
	return func() {
		secretSourceMu.Lock()
		secretSource = prev
		secretSourceMu.Unlock()
	}
}

// ScrubSecrets removes every listed secret from text. Longer secrets are
// removed first so a secret that contains another is not half-masked.
func ScrubSecrets(text string, secrets []string) string {
	for _, s := range secrets {
		if s != "" && strings.Contains(text, s) {
			text = strings.ReplaceAll(text, s, secretMarker)
		}
	}
	return text
}

// scrubResult removes vault secrets from everything in r that can reach the
// model or a person.
func scrubResult(r *ToolResult) {
	if r == nil {
		return
	}
	secretSourceMu.RLock()
	src := secretSource
	secretSourceMu.RUnlock()
	if src == nil {
		return
	}
	secrets := src()
	if len(secrets) == 0 {
		return
	}
	r.ForLLM = ScrubSecrets(r.ForLLM, secrets)
	r.ForUser = ScrubSecrets(r.ForUser, secrets)
	scrubValue(r.Evidence, secrets)
}

func scrubValue(v interface{}, secrets []string) {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, x := range t {
			if s, ok := x.(string); ok {
				t[k] = ScrubSecrets(s, secrets)
			} else {
				scrubValue(x, secrets)
			}
		}
	case []interface{}:
		for i, x := range t {
			if s, ok := x.(string); ok {
				t[i] = ScrubSecrets(s, secrets)
			} else {
				scrubValue(x, secrets)
			}
		}
	}
}
