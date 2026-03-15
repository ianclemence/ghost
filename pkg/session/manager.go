package session

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/rag"
)

type Session struct {
	Key      string              `json:"key"`
	Messages []providers.Message `json:"messages"`
	Summary  string              `json:"summary,omitempty"`
	Created  time.Time           `json:"created"`
	Updated  time.Time           `json:"updated"`
}

type SessionManager struct {
	store Store
	rag   *rag.Store
	mu    sync.RWMutex
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
	sm.store.Save(key)
}

// GetContext retrieves relevant context for the current turn (RAG)
// This can be used by ContextBuilder to inject RAG context
func (sm *SessionManager) Store() Store {
	return sm.store
}

func (sm *SessionManager) GetContext(ctx context.Context, userQuery string) string {
	if sm.rag == nil || userQuery == "" {
		return ""
	}
	results, err := sm.rag.Retrieve(ctx, userQuery, 3) // Top 3
	if err != nil || len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Relevant Context from Memory:\n")
	for _, r := range results {
		sb.WriteString("- " + r.Content + " (Source: " + r.Source + ")\n")
	}
	return sb.String()
}
