package session

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/rag"
	"github.com/ianclemence/ghost/pkg/retrieval"
)

type Session struct {
	Key      string              `json:"key"`
	Title    string              `json:"title,omitempty"`
	Messages []providers.Message `json:"messages"`
	Summary  string              `json:"summary,omitempty"`
	Created  time.Time           `json:"created"`
	Updated  time.Time           `json:"updated"`
}

type SessionManager struct {
	store Store
	rag   *rag.Store
	mu    sync.RWMutex
	// resourceScale returns a retrieval-budget multiplier (1.0 normal,
	// lower under memory/disk pressure). Nil = 1.0.
	resourceScale func() float64
}

// SetResourceScale installs the adaptive budget multiplier used to reduce
// retrieval under resource pressure. Nil-safe.
func (sm *SessionManager) SetResourceScale(fn func() float64) {
	sm.resourceScale = fn
}

func (sm *SessionManager) scale() float64 {
	if sm.resourceScale == nil {
		return 1
	}
	s := sm.resourceScale()
	if s <= 0 {
		return 1
	}
	return s
}

func NewSessionManager(store Store, ragStore *rag.Store) *SessionManager {
	return &SessionManager{
		store: store,
		rag:   ragStore,
	}
}

func (sm *SessionManager) AddMessage(sessionKey, role, content string) {
	sm.AddFullMessage(sessionKey, providers.Message{
		Role:    role,
		Content: content,
	})
}

func (sm *SessionManager) AddFullMessage(sessionKey string, msg providers.Message) {
	if sm.store == nil {
		return
	}
	sm.store.AddFullMessage(sessionKey, msg)

	// Generate title from the first user message.
	if msg.Role == "user" {
		existing := sm.store.GetTitle(sessionKey)
		if existing == "" {
			title := GenerateTitle(msg.Content)
			sm.store.SetTitle(sessionKey, title)
		}
	}
}

func (sm *SessionManager) GetHistory(key string) []providers.Message {
	if sm.store == nil {
		return []providers.Message{}
	}
	return sm.store.GetHistory(key)
}

func (sm *SessionManager) GetSummary(key string) string {
	if sm.store == nil {
		return ""
	}
	return sm.store.GetSummary(key)
}

func (sm *SessionManager) SetSummary(key string, summary string) {
	if sm.store == nil {
		return
	}
	sm.store.SetSummary(key, summary)
}

func (sm *SessionManager) GetTitle(key string) string {
	if sm.store == nil {
		return ""
	}
	return sm.store.GetTitle(key)
}

func (sm *SessionManager) SetTitle(key string, title string) {
	if sm.store == nil {
		return
	}
	sm.store.SetTitle(key, title)
}

func (sm *SessionManager) TruncateHistory(key string, keepLast int) {
	if sm.store == nil {
		return
	}
	sm.store.TruncateHistory(key, keepLast)
}

func (sm *SessionManager) Save(key string) error {
	if sm.store == nil {
		return nil
	}
	return sm.store.Save(key)
}

func (sm *SessionManager) SetHistory(key string, messages []providers.Message) {
	if sm.store == nil {
		return
	}
	sm.store.SetHistory(key, messages)
}

func (sm *SessionManager) ClearHistory(key string) {
	if sm.store == nil {
		return
	}
	sm.store.TruncateHistory(key, 0)
	sm.store.SetSummary(key, "")
	sm.store.SetTitle(key, "")
	sm.store.Save(key)
}

// DeleteSession permanently removes all evidence for a session.
func (sm *SessionManager) DeleteSession(key string) error {
	if sm.store == nil {
		return nil
	}
	return sm.store.DeleteSession(key)
}

// GetContext retrieves relevant context for the current turn (RAG)
// This can be used by ContextBuilder to inject RAG context
func (sm *SessionManager) Store() Store {
	return sm.store
}

func (sm *SessionManager) GetContext(ctx context.Context, userQuery string, scopes []string) string {
	if sm.rag == nil || userQuery == "" {
		return ""
	}
	results, err := sm.rag.RetrieveScoped(ctx, userQuery, 5, scopes) // over-fetch, then budget
	if err != nil || len(results) == 0 {
		return ""
	}

	// Context budget: the retrieved items are already ranked; keep only what
	// fits the memory-context budget so a long tail of weak matches cannot
	// crowd out the model's actual task.
	items := make([]string, 0, len(results))
	for _, r := range results {
		items = append(items, "- "+r.Content+" (Source: "+r.Source+")")
	}
	items = retrieval.DefaultBudget().Fit(items)
	if len(items) == 0 {
		return ""
	}
	// Under resource pressure, keep only the highest-ranked items so a
	// constrained device spends less on context it cannot afford.
	if scale := sm.scale(); scale < 1 {
		keep := int(float64(len(items))*scale + 0.5)
		if keep < 1 {
			keep = 1
		}
		if keep < len(items) {
			items = items[:keep]
		}
	}
	return "Relevant Context from Memory:\n" + strings.Join(items, "\n")
}
