package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var (
	sensitivePattern = regexp.MustCompile(`(?i)(api[_-]?key|secret|password|token|credential|authorization)\s*[:=]\s*\S+`)
	emailPattern     = regexp.MustCompile(`\b[\w.-]+@[\w.-]+\.\w+\b`)
)

func RedactTranscript(text string) string {
	result := sensitivePattern.ReplaceAllString(text, "${1}=[REDACTED]")
	result = emailPattern.ReplaceAllString(result, "[EMAIL]")
	return result
}

type TranscriptLine struct {
	Timestamp time.Time
	Role      string
	Text      string
}

type TranscriptManager struct {
	workspace string
	mu        sync.Mutex
}

func NewTranscriptManager(workspace string) *TranscriptManager {
	return &TranscriptManager{workspace: workspace}
}

func (tm *TranscriptManager) logsDir() string {
	return filepath.Join(tm.workspace, "logs")
}

func (tm *TranscriptManager) EnsureLogsDir() error {
	return os.MkdirAll(tm.logsDir(), 0700)
}

func (tm *TranscriptManager) transcriptPath(agentID string) string {
	return filepath.Join(tm.logsDir(), fmt.Sprintf("subagent-%s.log", agentID))
}

func (tm *TranscriptManager) manifestPath() string {
	return filepath.Join(tm.logsDir(), "manifest.json")
}

func (tm *TranscriptManager) WriteLine(agentID, role, text string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if err := tm.EnsureLogsDir(); err != nil {
		return err
	}

	redacted := RedactTranscript(text)
	ts := time.Now().Format("15:04:05")
	line := fmt.Sprintf("%s %s | %s\n", ts, role, redacted)

	path := tm.transcriptPath(agentID)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(line)
	return err
}

func (tm *TranscriptManager) ReadLines(agentID string, maxLines int) ([]TranscriptLine, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	path := tm.transcriptPath(agentID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	lines := splitLines(string(data))
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	var result []TranscriptLine
	for _, line := range lines {
		if tl := parseTranscriptLine(line); tl != nil {
			result = append(result, *tl)
		}
	}
	return result, nil
}

func (tm *TranscriptManager) ReadRaw(agentID string, maxChars int) (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	path := tm.transcriptPath(agentID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	content := string(data)
	if maxChars > 0 && len(content) > maxChars {
		content = content[len(content)-maxChars:]
	}
	return content, nil
}

func (tm *TranscriptManager) ListAgents() ([]string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if err := tm.EnsureLogsDir(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(tm.logsDir())
	if err != nil {
		return nil, err
	}

	var agents []string
	for _, entry := range entries {
		name := entry.Name()
		if len(name) > 10 && name[:10] == "subagent-" && name[len(name)-4:] == ".log" {
			agentID := name[10 : len(name)-4]
			agents = append(agents, agentID)
		}
	}
	return agents, nil
}

func (tm *TranscriptManager) DeleteTranscript(agentID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	path := tm.transcriptPath(agentID)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func splitLines(s string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

func parseTranscriptLine(line string) *TranscriptLine {
	if len(line) < 10 {
		return nil
	}

	parts := splitN(line, " | ", 2)
	if len(parts) < 2 {
		return nil
	}

	tsRolePart := parts[0]
	text := parts[1]

	// Format: "HH:MM:SS role" - split on first space after timestamp
	spaceIdx := -1
	for i := 0; i < len(tsRolePart); i++ {
		if tsRolePart[i] == ' ' {
			spaceIdx = i
			break
		}
	}

	role := "unknown"
	tsStr := tsRolePart
	if spaceIdx > 0 {
		tsStr = tsRolePart[:spaceIdx]
		role = tsRolePart[spaceIdx+1:]
	}

	ts, err := time.Parse("15:04:05", tsStr)
	if err != nil {
		return nil
	}

	return &TranscriptLine{
		Timestamp: ts,
		Role:      role,
		Text:      text,
	}
}

func splitN(s, sep string, n int) []string {
	idx := 0
	var parts []string
	for i := 0; i < n-1; i++ {
		pos := indexOf(s[idx:], sep)
		if pos < 0 {
			break
		}
		parts = append(parts, s[idx:idx+pos])
		idx += pos + len(sep)
	}
	parts = append(parts, s[idx:])
	return parts
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
