package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/logger"
)

// OracleTool bundles multiple files and a prompt into a single "context package" for the LLM.
// Adapted from OpenClaw's oracle skill.
type OracleTool struct {
	workspace string
	restrict  bool
}

func NewOracleTool(workspace string, restrict bool) *OracleTool {
	return &OracleTool{
		workspace: workspace,
		restrict:  restrict,
	}
}

func (t *OracleTool) Name() string {
	return "oracle"
}

func (t *OracleTool) Description() string {
	return "Bundle multiple files and a task description into a single context package for deep analysis. Supports globs and exclusions."
}

func (t *OracleTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task": map[string]interface{}{
				"type":        "string",
				"description": "The specific task or question you want the LLM to address using these files.",
			},
			"files": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": "List of files, directories, or glob patterns to include. Use '!' prefix to exclude (e.g., '!**/*.test.go').",
			},
		},
		"required": []string{"task", "files"},
	}
}

func (t *OracleTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	task, _ := args["task"].(string)
	files, _ := args["files"].([]interface{})

	if task == "" {
		return ErrorResult("task is required")
	}
	if len(files) == 0 {
		return ErrorResult("at least one file or pattern is required")
	}

	var includedFiles []string
	var excludedPatterns []string

	for _, f := range files {
		pattern, ok := f.(string)
		if !ok {
			continue
		}
		if strings.HasPrefix(pattern, "!") {
			excludedPatterns = append(excludedPatterns, strings.TrimPrefix(pattern, "!"))
		} else {
			matches, err := t.resolvePattern(pattern)
			if err != nil {
				logger.WarnCF("oracle", "Failed to resolve pattern", map[string]interface{}{"pattern": pattern, "error": err})
				continue
			}
			includedFiles = append(includedFiles, matches...)
		}
	}

	// Filter excluded files
	finalFiles := t.filterExclusions(includedFiles, excludedPatterns)

	if len(finalFiles) == 0 {
		return ErrorResult("No files matched the provided patterns.")
	}

	// Build the context bundle
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Oracle Context Bundle\n\n## Task\n%s\n\n## Included Files (%d)\n\n", task, len(finalFiles)))

	for _, path := range finalFiles {
		content, err := os.ReadFile(path)
		if err != nil {
			sb.WriteString(fmt.Sprintf("### FILE: %s\nERROR: Failed to read file: %v\n\n", path, err))
			continue
		}

		relPath, _ := filepath.Rel(t.workspace, path)
		sb.WriteString(fmt.Sprintf("### FILE: %s\n\n```\n%s\n```\n\n", relPath, string(content)))
	}

	return NewToolResult(sb.String())
}

func (t *OracleTool) resolvePattern(pattern string) ([]string, error) {
	// Simple implementation: if it's a direct path, use it. If it's a glob, use filepath.Glob.
	// In a real implementation, we would use a more robust globbing library like doublestar.

	resolvedPath, err := validatePath(pattern, t.workspace, t.restrict)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(resolvedPath)
	if err == nil && !info.IsDir() {
		return []string{resolvedPath}, nil
	}

	// If it's a directory, list all files recursively (limited)
	if err == nil && info.IsDir() {
		var matches []string
		err := filepath.Walk(resolvedPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			matches = append(matches, path)
			return nil
		})
		return matches, err
	}

	// Try as a glob
	matches, err := filepath.Glob(resolvedPath)
	if err != nil {
		return nil, err
	}

	return matches, nil
}

func (t *OracleTool) filterExclusions(files []string, patterns []string) []string {
	if len(patterns) == 0 {
		return files
	}

	var result []string
	for _, file := range files {
		excluded := false
		for _, pattern := range patterns {
			if matched, _ := filepath.Match(pattern, filepath.Base(file)); matched {
				excluded = true
				break
			}
			if strings.Contains(file, pattern) {
				excluded = true
				break
			}
		}
		if !excluded {
			result = append(result, file)
		}
	}
	return result
}
