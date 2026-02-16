// Ghost - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 Ghost contributors

package agent

import (
	"os"
	"path/filepath"
)

// MemoryStore manages persistent memory for the agent.
// - Long-term memory: memory/MEMORY.md
type MemoryStore struct {
	workspace string
}

func NewMemoryStore(workspace string) *MemoryStore {
	return &MemoryStore{
		workspace: workspace,
	}
}

// GetMemoryContext returns the content of MEMORY.md
func (m *MemoryStore) GetMemoryContext() string {
	memoryPath := filepath.Join(m.workspace, "memory", "MEMORY.md")
	content, err := os.ReadFile(memoryPath)
	if err != nil {
		return ""
	}
	return string(content)
}
