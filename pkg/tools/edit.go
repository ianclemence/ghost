package tools

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// EditFileTool edits a file by replacing old_text with new_text.
// The old_text must exist exactly in the file.
type EditFileTool struct {
	allowedDir string
	restrict   bool
	guard      ScopeGuard
}

// NewEditFileTool creates a new EditFileTool with optional directory restriction.
func NewEditFileTool(allowedDir string, restrict bool) *EditFileTool {
	return &EditFileTool{
		allowedDir: allowedDir,
		restrict:   restrict,
	}
}

// SetScopeGuard installs the memory privacy boundary (inert by default).
func (t *EditFileTool) SetScopeGuard(g ScopeGuard) { t.guard = g }

func (t *EditFileTool) Name() string {
	return "edit_file"
}

func (t *EditFileTool) Description() string {
	return "Edit a file by replacing old_text with new_text. The old_text must exist exactly in the file."
}

func (t *EditFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The file path to edit",
			},
			"old_text": map[string]interface{}{
				"type":        "string",
				"description": "The exact text to find and replace",
			},
			"new_text": map[string]interface{}{
				"type":        "string",
				"description": "The text to replace with",
			},
		},
		"required": []string{"path", "old_text", "new_text"},
	}
}

func (t *EditFileTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	path, ok := args["path"].(string)
	if !ok {
		return ErrorResult("path is required")
	}

	oldText, ok := args["old_text"].(string)
	if !ok {
		return ErrorResult("old_text is required")
	}

	newText, ok := args["new_text"].(string)
	if !ok {
		return ErrorResult("new_text is required")
	}

	resolvedPath, err := validatePath(path, t.allowedDir, t.restrict)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := guardPrimaryFileIn(t.allowedDir, resolvedPath); err != nil {
		return ErrorResult(err.Error())
	}
	if err := guardGhostEstateWrite(t.allowedDir, resolvedPath); err != nil {
		return ErrorResult(err.Error())
	}
	// Editing reads the current content first: gate as a read.
	if err := t.guard.Check(SessionKeyFromContext(ctx), OpRead, resolvedPath); err != nil {
		return ErrorResult(err.Error())
	}

	if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
		return ErrorResult(fmt.Sprintf("file not found: %s", path))
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read file: %v", err))
	}

	contentStr := string(content)

	start, end, fuzzy, problem := editSpan(contentStr, oldText)
	if problem != "" {
		return ErrorResult(problem)
	}

	// Keep the file's line endings: a new_text pasted with LF into a CRLF
	// file should not leave one edited block with different endings.
	if strings.Contains(contentStr, "\r\n") {
		newText = strings.ReplaceAll(strings.ReplaceAll(newText, "\r\n", "\n"), "\n", "\r\n")
	}

	newContent := contentStr[:start] + newText + contentStr[end:]

	if err := os.WriteFile(resolvedPath, []byte(newContent), 0644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write file: %v", err))
	}

	note := fmt.Sprintf("File edited: %s", path)
	if fuzzy {
		note += " (matched after normalising whitespace)"
	}
	res := SilentResult(note)
	res.Evidence = FileWriteEvidenceFromDisk(resolvedPath)
	return res
}

// editSpan finds old_text in content and returns the byte span to replace.
//
// A model's old_text goes stale in ordinary ways: it remembers the indentation
// differently, drops a trailing space, or carries LF into a CRLF file. Failing
// those edits costs seconds of silence and often a whole turn, so after an
// exact match fails the text is matched again ignoring whitespace. The match
// must still be unique; an ambiguous edit is refused rather than guessed.
func editSpan(content, oldText string) (start, end int, fuzzy bool, problem string) {
	if oldText == "" {
		return 0, 0, false, "old_text is empty; provide the text to replace"
	}
	if idx := strings.Index(content, oldText); idx >= 0 {
		count := strings.Count(content, oldText)
		if count > 1 {
			return 0, 0, false, fmt.Sprintf("old_text appears %d times. Please provide more context to make it unique", count)
		}
		return idx, idx + len(oldText), false, ""
	}
	// The same text with the other line ending is still the same edit.
	for _, variant := range lineEndingVariants(content, oldText) {
		if idx := strings.Index(content, variant); idx >= 0 && strings.Count(content, variant) == 1 {
			return idx, idx + len(variant), false, ""
		}
	}
	if strings.TrimSpace(oldText) == "" {
		return 0, 0, false, "old_text is only whitespace; provide the text to replace"
	}
	re, err := regexp.Compile(fuzzyEditPattern(oldText))
	if err != nil {
		return 0, 0, false, "old_text not found in file. Make sure it matches exactly"
	}
	matches := re.FindAllStringIndex(content, -1)
	switch len(matches) {
	case 1:
		return matches[0][0], matches[0][1], true, ""
	case 0:
		return 0, 0, false, "old_text not found in file. The file may have changed since you read it: read it again and anchor to the current text (or use write_file to replace the whole file)."
	default:
		return 0, 0, false, fmt.Sprintf("old_text does not match exactly and appears %d times when whitespace is ignored. Add more surrounding lines to make it unique", len(matches))
	}
}

// lineEndingVariants rewrites oldText with the file's line endings, so a
// CRLF/LF mismatch does not fail an otherwise exact edit.
func lineEndingVariants(content, oldText string) []string {
	hasCRLF := strings.Contains(content, "\r\n")
	hasLF := strings.Contains(strings.ReplaceAll(content, "\r\n", ""), "\n")
	var out []string
	if hasCRLF && !strings.Contains(oldText, "\r\n") {
		out = append(out, strings.ReplaceAll(oldText, "\n", "\r\n"))
	}
	if hasLF && strings.Contains(oldText, "\r\n") {
		out = append(out, strings.ReplaceAll(oldText, "\r\n", "\n"))
	}
	return out
}

// fuzzyEditPattern matches old_text with any indentation, any run of spaces or
// tabs between words, and either line ending. Literal text is quoted, so the
// only looseness is whitespace.
func fuzzyEditPattern(oldText string) string {
	old := strings.ReplaceAll(oldText, "\r\n", "\n")
	lines := strings.Split(old, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			parts = append(parts, `[ \t]*`)
			continue
		}
		quoted := make([]string, len(fields))
		for i, f := range fields {
			quoted[i] = regexp.QuoteMeta(f)
		}
		parts = append(parts, `[ \t]*`+strings.Join(quoted, `[ \t]+`)+`[ \t]*`)
	}
	return strings.Join(parts, `\r?\n`)
}

type AppendFileTool struct {
	workspace string
	restrict  bool
	guard     ScopeGuard
}

func NewAppendFileTool(workspace string, restrict bool) *AppendFileTool {
	return &AppendFileTool{workspace: workspace, restrict: restrict}
}

// SetScopeGuard installs the memory privacy boundary (inert by default).
func (t *AppendFileTool) SetScopeGuard(g ScopeGuard) { t.guard = g }

func (t *AppendFileTool) Name() string {
	return "append_file"
}

func (t *AppendFileTool) Description() string {
	return "Append content to the end of an existing file. Use when: accumulating notes (shopping list, journaling, reminders) without overwriting. Returns the new file length."
}

func (t *AppendFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path of the file to append to. Example: \"notes/shopping-list.md\".",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Text to append (a new line is added).",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *AppendFileTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
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
	if err := guardPrimaryFileIn(t.workspace, resolvedPath); err != nil {
		return ErrorResult(err.Error())
	}
	if err := guardGhostEstateWrite(t.workspace, resolvedPath); err != nil {
		return ErrorResult(err.Error())
	}
	if err := t.guard.Check(SessionKeyFromContext(ctx), OpWrite, resolvedPath); err != nil {
		return ErrorResult(err.Error())
	}

	f, err := os.OpenFile(resolvedPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to open file: %v", err))
	}
	defer f.Close()

	if _, err := f.WriteString(content); err != nil {
		return ErrorResult(fmt.Sprintf("failed to append to file: %v", err))
	}

	res := SilentResult(fmt.Sprintf("Appended to %s", path))
	res.Evidence = FileWriteEvidenceFromDisk(resolvedPath)
	return res
}

// Verify confirms the append actually persisted at the end of the file (Phase 3).
func (t *AppendFileTool) Verify(ctx context.Context, args map[string]interface{}) error {
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
	if !strings.HasSuffix(string(data), content) {
		return fmt.Errorf("appended content was not found at the end of %q", path)
	}
	return nil
}
