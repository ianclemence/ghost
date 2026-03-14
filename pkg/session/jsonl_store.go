package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/providers"
)

type JSONLStore struct {
	baseDir string
	mu      sync.Mutex
}

type jsonlEntry struct {
	Role         string                 `json:"role"`
	Content      string                 `json:"content"`
	MultiContent []providers.ContentPart `json:"multi_content,omitempty"`
	ToolCallID   string                 `json:"tool_call_id,omitempty"`
	ToolCalls    []providers.ToolCall   `json:"tool_calls,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
}

func NewJSONLStore(baseDir string) *JSONLStore {
	return &JSONLStore{baseDir: baseDir}
}

func (s *JSONLStore) EnsureSession(key string) {
	dir := filepath.Join(s.baseDir, "sessions")
	os.MkdirAll(dir, 0755)
}

func (s *JSONLStore) AddFullMessage(sessionKey string, msg providers.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EnsureSession(sessionKey)
	path := s.sessionPath(sessionKey)
	if path == "" {
		return
	}
	entry := jsonlEntry{
		Role:         msg.Role,
		Content:      msg.Content,
		MultiContent: msg.MultiContent,
		ToolCallID:   msg.ToolCallID,
		ToolCalls:    msg.ToolCalls,
		CreatedAt:    time.Now(),
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	enc.Encode(entry)
}

func (s *JSONLStore) GetHistory(key string) []providers.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.sessionPath(key)
	if path == "" {
		return []providers.Message{}
	}
	file, err := os.Open(path)
	if err != nil {
		return []providers.Message{}
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var history []providers.Message
	for scanner.Scan() {
		var entry jsonlEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		msg := providers.Message{
			Role:         entry.Role,
			Content:      entry.Content,
			MultiContent: entry.MultiContent,
			ToolCallID:   entry.ToolCallID,
			ToolCalls:    entry.ToolCalls,
		}
		history = append(history, msg)
	}
	return history
}

func (s *JSONLStore) GetSummary(key string) string {
	path := s.summaryPath(key)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func (s *JSONLStore) SetSummary(key string, summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EnsureSession(key)
	path := s.summaryPath(key)
	if path == "" {
		return
	}
	os.WriteFile(path, []byte(summary), 0644)
}

func (s *JSONLStore) TruncateHistory(key string, keepLast int) {
	history := s.GetHistory(key)
	if keepLast <= 0 {
		history = []providers.Message{}
	} else if len(history) > keepLast {
		history = history[len(history)-keepLast:]
	}
	s.SetHistory(key, history)
}

func (s *JSONLStore) SetHistory(key string, messages []providers.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EnsureSession(key)
	path := s.sessionPath(key)
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	for _, msg := range messages {
		entry := jsonlEntry{
			Role:         msg.Role,
			Content:      msg.Content,
			MultiContent: msg.MultiContent,
			ToolCallID:   msg.ToolCallID,
			ToolCalls:    msg.ToolCalls,
			CreatedAt:    time.Now(),
		}
		enc.Encode(entry)
	}
}

func (s *JSONLStore) Save(key string) error {
	history := s.GetHistory(key)
	s.SetHistory(key, history)
	return nil
}

func (s *JSONLStore) sessionPath(key string) string {
	key = sanitizeSessionKey(key)
	if key == "" {
		return ""
	}
	return filepath.Join(s.baseDir, "sessions", key+".jsonl")
}

func (s *JSONLStore) summaryPath(key string) string {
	key = sanitizeSessionKey(key)
	if key == "" {
		return ""
	}
	return filepath.Join(s.baseDir, "sessions", key+".summary")
}

func sanitizeSessionKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	key = strings.ReplaceAll(key, ":", "_")
	key = strings.ReplaceAll(key, "/", "_")
	key = strings.ReplaceAll(key, "\\", "_")
	if key == "." || key == ".." {
		return ""
	}
	return key
}
