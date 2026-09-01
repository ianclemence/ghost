// Ghost - Ultra-lightweight personal AI agent
// Inspired by and based on GHOST: https://github.com/ianclemence/ghost
// License: MIT
//
// Copyright (c) 2026 Ghost contributors

package agent

import (
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MemoryStore manages persistent memory for the agent.
// - Long-term memory: memory/MEMORY.md
// - Daily notes: memory/YYYYMM/YYYYMMDD.md
type MemoryStore struct {
	workspace  string
	memoryDir  string
	memoryFile string
}

// NewMemoryStore creates a new MemoryStore with the given workspace path.
// It ensures the memory directory exists.
func NewMemoryStore(workspace string) *MemoryStore {
	memoryDir := filepath.Join(workspace, "memory")
	memoryFile := filepath.Join(memoryDir, "MEMORY.md")

	// Ensure memory directory exists
	os.MkdirAll(memoryDir, 0755)

	return &MemoryStore{
		workspace:  workspace,
		memoryDir:  memoryDir,
		memoryFile: memoryFile,
	}
}

// getTodayFile returns the path to today's daily note file (memory/YYYYMM/YYYYMMDD.md).
func (ms *MemoryStore) getTodayFile() string {
	today := time.Now().Format("20060102") // YYYYMMDD
	monthDir := today[:6]                  // YYYYMM
	filePath := filepath.Join(ms.memoryDir, monthDir, today+".md")
	return filePath
}

// ReadLongTerm reads the long-term memory (MEMORY.md).
// Returns empty string if the file doesn't exist.
func (ms *MemoryStore) ReadLongTerm() string {
	if data, err := os.ReadFile(ms.memoryFile); err == nil {
		return string(data)
	}
	return ""
}

// WriteLongTerm writes content to the long-term memory file (MEMORY.md).
func (ms *MemoryStore) WriteLongTerm(content string) error {
	return os.WriteFile(ms.memoryFile, []byte(content), 0644)
}

// ReadToday reads today's daily note.
// Returns empty string if the file doesn't exist.
func (ms *MemoryStore) ReadToday() string {
	todayFile := ms.getTodayFile()
	if data, err := os.ReadFile(todayFile); err == nil {
		return string(data)
	}
	return ""
}

// AppendToday appends content to today's daily note.
// If the file doesn't exist, it creates a new file with a date header.
func (ms *MemoryStore) AppendToday(content string) error {
	todayFile := ms.getTodayFile()

	// Ensure month directory exists
	monthDir := filepath.Dir(todayFile)
	os.MkdirAll(monthDir, 0755)

	var existingContent string
	if data, err := os.ReadFile(todayFile); err == nil {
		existingContent = string(data)
	}

	var newContent string
	if existingContent == "" {
		// Add header for new day
		header := fmt.Sprintf("# %s\n\n", time.Now().Format("2006-01-02"))
		newContent = header + content
	} else {
		// Append to existing content
		newContent = existingContent + "\n" + content
	}

	return os.WriteFile(todayFile, []byte(newContent), 0644)
}

// GetRecentDailyNotes returns daily notes from the last N days.
// Contents are joined with "---" separator.
func (ms *MemoryStore) GetRecentDailyNotes(days int) string {
	var notes []string

	for i := 0; i < days; i++ {
		date := time.Now().AddDate(0, 0, -i)
		dateStr := date.Format("20060102") // YYYYMMDD
		monthDir := dateStr[:6]            // YYYYMM
		filePath := filepath.Join(ms.memoryDir, monthDir, dateStr+".md")

		if data, err := os.ReadFile(filePath); err == nil {
			notes = append(notes, string(data))
		}
	}

	if len(notes) == 0 {
		return ""
	}

	// Join with separator
	var result string
	for i, note := range notes {
		if i > 0 {
			result += "\n\n---\n\n"
		}
		result += note
	}
	return result
}

// GetMemoryContext returns formatted memory context for the agent prompt.
// Includes long-term memory and recent daily notes.
func (ms *MemoryStore) GetMemoryContext() string {
	var parts []string

	// Long-term memory
	longTerm := ms.ReadLongTerm()
	if longTerm != "" {
		parts = append(parts, "## Long-term Memory\n\n"+longTerm)
	}

	// Recent daily notes (last 3 days)
	recentNotes := ms.GetRecentDailyNotes(3)
	if recentNotes != "" {
		parts = append(parts, "## Recent Daily Notes\n\n"+recentNotes)
	}

	if len(parts) == 0 {
		return ""
	}

	// Join parts with separator
	var result string
	for i, part := range parts {
		if i > 0 {
			result += "\n\n---\n\n"
		}
		result += part
	}
	return fmt.Sprintf("# Memory\n\n%s", result)
}

// MemoryHit is one search result over the memory notes.
type MemoryHit struct {
	Path     string    `json:"path"`
	Excerpt  string    `json:"excerpt"`
	Score    float64   `json:"score"`
	Modified time.Time `json:"modified"`
}

// Search retrieves the most relevant memory notes (daily notes, MEMORY.md,
// captures) for a query using keyword relevance + recency. It deliberately uses
// the existing on-disk notes and simple scoring — no embeddings, no external
// vector index — so it stays local, cheap, and explainable. This is the
// targeted long-tail retrieval path: the agent calls it when the digest doesn't
// cover what it needs.
func (ms *MemoryStore) Search(query string, limit int) []MemoryHit {
	if limit <= 0 {
		limit = 5
	}
	words := tokenizeSearch(query)
	if len(words) == 0 {
		return nil
	}
	if _, err := os.Stat(ms.memoryDir); err != nil {
		return nil
	}
	now := time.Now()
	var out []MemoryHit
	_ = filepath.WalkDir(ms.memoryDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		data, _ := os.ReadFile(path)
		if len(data) > 4*1024*1024 { // skip oversized notes
			return nil
		}
		lower := strings.ToLower(string(data))
		hits := 0
		for _, w := range words {
			hits += strings.Count(lower, w)
		}
		if hits == 0 {
			return nil
		}
		info, _ := d.Info()
		score := float64(hits) + recencyBoost(now, info.ModTime())
		out = append(out, MemoryHit{
			Path:     path,
			Excerpt:  excerptFor(lower, words),
			Score:    score,
			Modified: info.ModTime(),
		})
		return nil
	})

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if !out[i].Modified.Equal(out[j].Modified) {
			return out[i].Modified.After(out[j].Modified)
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// tokenizeSearch splits a query into lowercase search words (len >= 2).
func tokenizeSearch(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	words := fields[:0]
	for _, f := range fields {
		f = strings.Trim(f, ".,;:!?\"'()[]{}-_")
		if len([]rune(f)) >= 2 {
			words = append(words, f)
		}
	}
	return words
}

// recencyBoost favours recently-written notes, decaying over ~30 days.
func recencyBoost(now, modified time.Time) float64 {
	age := now.Sub(modified).Hours()
	if age < 0 {
		age = 0
	}
	return math.Exp(-age / (30 * 24))
}

// excerptFor returns a single-line excerpt around the first keyword hit.
func excerptFor(lower string, words []string) string {
	idx := -1
	for _, w := range words {
		if i := strings.Index(lower, w); i >= 0 {
			if idx < 0 || i < idx {
				idx = i
			}
		}
	}
	if idx < 0 {
		idx = 0
	}
	start := idx - 120
	if start < 0 {
		start = 0
	}
	end := idx + 200
	if end > len(lower) {
		end = len(lower)
	}
	s := lower[start:end]
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}
