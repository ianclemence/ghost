package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// under reports whether p is root or a descendant of root, using clean path
// containment on a path-separator boundary. This is the correct confinement
// check: raw strings.HasPrefix would let a sibling whose name merely starts
// with root's path (e.g. "workspace-evil") escape the sandbox.
func under(root, p string) bool {
	root = filepath.Clean(root)
	p = filepath.Clean(p)
	return p == root || strings.HasPrefix(p, root+string(os.PathSeparator))
}

// validatePath ensures the given path is within the workspace if restrict is true.
func validatePath(path, workspace string, restrict bool) (string, error) {
	if workspace == "" {
		return path, nil
	}

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace path: %w", err)
	}

	var absPath string
	if filepath.IsAbs(path) {
		absPath = filepath.Clean(path)
	} else {
		absPath, err = filepath.Abs(filepath.Join(absWorkspace, path))
		if err != nil {
			return "", fmt.Errorf("failed to resolve file path: %w", err)
		}
	}

	if restrict {
		// Allow access to temporary media files from mobile uploads.
		tempMediaDir := filepath.Join(os.TempDir(), "GHOST_media")
		if under(tempMediaDir, absPath) {
			return absPath, nil
		}

		// Allow access to the workspace and its descendants.
		if under(absWorkspace, absPath) {
			return absPath, nil
		}

		// Allow access to the project root (parent of workspace), so the agent
		// can work on its own source tree during development. Confinement is
		// still enforced on a path-separator boundary (under), so a sibling
		// whose name merely starts with the workspace or project path cannot
		// escape.
		projectRoot := filepath.Dir(absWorkspace)
		if under(projectRoot, absPath) {
			return absPath, nil
		}

		return "", fmt.Errorf("access denied: path is outside the workspace")
	}

	return absPath, nil
}

type ReadFileTool struct {
	workspace string
	restrict  bool
}

func NewReadFileTool(workspace string, restrict bool) *ReadFileTool {
	return &ReadFileTool{workspace: workspace, restrict: restrict}
}

func (t *ReadFileTool) Name() string {
	return "read_file"
}

func (t *ReadFileTool) Description() string {
	return "Read a file (or a skill SKILL.md) and return its contents. Use when: you need local file content, or you must read a skill\u2019s SKILL.md after choosing it. Do NOT use for: questions a skill already answers. Returns the file text (truncated for very large files)."
}

func (t *ReadFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Relative or absolute path. Example: \"skills/weather/SKILL.md\" or \"memory/MEMORY.md\".",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	path, ok := args["path"].(string)
	if !ok {
		return ErrorResult("path is required")
	}

	resolvedPath, err := validatePath(path, t.workspace, t.restrict)
	if err != nil {
		return ErrorResult(err.Error())
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read file: %v", err))
	}

	return NewToolResult(string(content))
}

type WriteFileTool struct {
	workspace string
	restrict  bool
}

func NewWriteFileTool(workspace string, restrict bool) *WriteFileTool {
	return &WriteFileTool{workspace: workspace, restrict: restrict}
}

func (t *WriteFileTool) Name() string {
	return "write_file"
}

func (t *WriteFileTool) Description() string {
	return "Create or overwrite a file with the given content. Use when: a skill or task asks you to save a file in the workspace. Returns confirmation with the path and byte count."
}

func (t *WriteFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to write to. Example: \"notes/reminders.md\".",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The full file content. Use \n for newlines.",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	path, ok := args["path"].(string)
	if !ok {
		return ErrorResult("path is required")
	}

	content, ok := args["content"].(string)
	if !ok {
		return ErrorResult("content is required")
	}

	resolvedPath, err := validatePath(path, t.workspace, t.restrict)
	if err != nil {
		return ErrorResult(err.Error())
	}

	dir := filepath.Dir(resolvedPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create directory: %v", err))
	}

	if err := os.WriteFile(resolvedPath, []byte(content), 0644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write file: %v", err))
	}

	return SilentResult(fmt.Sprintf("File written: %s", path))
}

// Verify confirms the written content actually landed on disk (Phase 3).
func (t *WriteFileTool) Verify(ctx context.Context, args map[string]interface{}) error {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return fmt.Errorf("path is required")
	}
	resolved, err := validatePath(path, t.workspace, t.restrict)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Errorf("could not read back %q: %w", path, err)
	}
	if !strings.Contains(string(data), content) {
		return fmt.Errorf("expected content was not found in %q", path)
	}
	return nil
}

type ListDirTool struct {
	workspace string
	restrict  bool
}

func NewListDirTool(workspace string, restrict bool) *ListDirTool {
	return &ListDirTool{workspace: workspace, restrict: restrict}
}

func (t *ListDirTool) Name() string {
	return "list_dir"
}

func (t *ListDirTool) Description() string {
	return "List files and directories in a path"
}

func (t *ListDirTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to list",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ListDirTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	path, ok := args["path"].(string)
	if !ok {
		path = "."
	}

	resolvedPath, err := validatePath(path, t.workspace, t.restrict)
	if err != nil {
		return ErrorResult(err.Error())
	}

	entries, err := os.ReadDir(resolvedPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read directory: %v", err))
	}

	result := ""
	for _, entry := range entries {
		if entry.IsDir() {
			result += "DIR:  " + entry.Name() + "\n"
		} else {
			result += "FILE: " + entry.Name() + "\n"
		}
	}

	return NewToolResult(result)
}
