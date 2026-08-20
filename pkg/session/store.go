package session

import "github.com/ianclemence/ghost/pkg/providers"

type Store interface {
	EnsureSession(key string)
	AddFullMessage(key string, msg providers.Message)
	GetHistory(key string) []providers.Message
	GetSummary(key string) string
	SetSummary(key string, summary string)
	TruncateHistory(key string, keepLast int)
	SetHistory(key string, messages []providers.Message)
	Save(key string) error
	DeleteSession(key string) error
}
