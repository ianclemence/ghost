package session

import "github.com/ianclemence/ghost/pkg/providers"

type Store interface {
	EnsureSession(key string)
	AddFullMessage(key string, msg providers.Message)
	// GetHistory is the model's context (compacted and deleted rows excluded).
	GetHistory(key string) []providers.Message
	// GetDisplayHistory is the owner's transcript (only deleted rows excluded).
	GetDisplayHistory(key string) []providers.Message
	GetSummary(key string) string
	SetSummary(key string, summary string)
	GetTitle(key string) string
	SetTitle(key string, title string)
	TruncateHistory(key string, keepLast int)
	SetHistory(key string, messages []providers.Message)
	Save(key string) error
	DeleteSession(key string) error
}
