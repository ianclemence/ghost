package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/suggest"
	"github.com/ianclemence/ghost/pkg/utils"
)

// What the owner is most likely to type next, offered in the empty message box.
//
// It is computed from the conversation as it stands, so it is a function of
// the last message: the same ending gives the same suggestion, and a new
// message gives a new one. Rules answer first (see package suggest); only when
// none applies is a small model call made, and only when the model is free,
// because a suggestion must never make a real turn wait. GHOST_SUGGEST turns
// the model off ("rules") or everything off ("off").

// Suggestion is a possible next message and where it came from.
type Suggestion struct {
	Text   string `json:"suggestion"`
	Source string `json:"source,omitempty"` // "rule" or "model"
	// For identifies the conversation state it was made for, so a client can
	// tell a stale answer from a current one.
	For string `json:"for,omitempty"`
}

type suggestCacheEntry struct {
	key string
	sug Suggestion
}

var (
	suggestMu     sync.Mutex
	suggestCache  = map[string]suggestCacheEntry{}
	suggestLocks  = map[string]*sync.Mutex{}
	suggestWindow = 6 // messages of context
)

func suggestLock(session string) *sync.Mutex {
	suggestMu.Lock()
	defer suggestMu.Unlock()
	l := suggestLocks[session]
	if l == nil {
		l = &sync.Mutex{}
		suggestLocks[session] = l
	}
	return l
}

// SuggestNext returns the suggestion for the session's current state, or an
// empty one. It never blocks behind a running turn and never errors: no
// suggestion is a normal answer.
func (al *AgentLoop) SuggestNext(ctx context.Context, session string) Suggestion {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("GHOST_SUGGEST")))
	if al == nil || mode == "off" || session == "" || isAutomationSession(session) {
		return Suggestion{}
	}
	turns := recentTurns(al.History(session), suggestWindow)
	if len(turns) == 0 || turns[len(turns)-1].Role != "assistant" {
		return Suggestion{} // Ghost is not waiting on the owner
	}
	last := turns[len(turns)-1].Text
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", len(turns), last)))
	key := hex.EncodeToString(sum[:6])

	lock := suggestLock(session)
	lock.Lock()
	defer lock.Unlock()
	suggestMu.Lock()
	if c, ok := suggestCache[session]; ok && c.key == key {
		suggestMu.Unlock()
		return c.sug
	}
	suggestMu.Unlock()

	sug := Suggestion{For: key}
	if text, ok := suggest.FromRules(last); ok {
		sug.Text, sug.Source = text, "rule"
	} else if mode != "rules" {
		if !al.backgroundModelAllowed() || al.provider == nil {
			return sug // busy: ask again later, and do not remember "none"
		}
		if text, ok := al.modelSuggestion(ctx, turns, last); ok {
			sug.Text, sug.Source = text, "model"
		}
	}
	suggestMu.Lock()
	suggestCache[session] = suggestCacheEntry{key: key, sug: sug}
	suggestMu.Unlock()
	return sug
}

func (al *AgentLoop) modelSuggestion(ctx context.Context, turns []suggest.Turn, last string) (string, bool) {
	system, user := suggest.Prompt(turns)
	callCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	resp, err := al.provider.Chat(callCtx, []providers.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}, nil, al.model, map[string]interface{}{"temperature": 0.3, "max_tokens": 24})
	if err != nil || resp == nil {
		return "", false
	}
	return suggest.Clean(resp.Content, last)
}

// recentTurns keeps the last n owner and Ghost messages, without tool calls or
// the internal date labels, oldest first.
func recentTurns(history []providers.Message, n int) []suggest.Turn {
	var out []suggest.Turn
	for _, m := range history {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		text := strings.TrimSpace(utils.StripDateStamp(m.Content))
		if text == "" {
			continue
		}
		out = append(out, suggest.Turn{Role: m.Role, Text: text})
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}
