package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ianclemence/ghost/pkg/logger"
)

// MemoryCurateTool provides bounded, curated memory that persists across sessions.
// Two stores:
//   - curated-memory.md: agent's personal notes (environment facts, project conventions, tool quirks)
//   - user-profile.md: what the agent knows about the user (preferences, communication style, habits)
//
// Both are injected into the system prompt. Character limits force the agent to curate,
// keeping only what actually matters. This complements Ghost's existing RAG memory system.
// Inspired by Hermes Agent's memory_tool.py.
type MemoryCurateTool struct {
	workspace string
	// ContextFor resolves the calling session's context (nil = legacy
	// global files). Set by the agent runtime; the model can never set it.
	ContextFor func(session string) string

	memoryCharLimit  int
	profileCharLimit int
}

const (
	defaultMemoryCharLimit  = 2200
	defaultProfileCharLimit = 1375
	entryDelimiter          = "\n§\n"
)

// Threat patterns to scan memory content for injection/exfiltration attempts
var memoryThreatPatterns = []struct {
	pattern *regexp.Regexp
	id      string
}{
	{regexp.MustCompile(`(?i)ignore\s+(previous|all|above|prior)\s+instructions`), "prompt_injection"},
	{regexp.MustCompile(`(?i)you\s+are\s+now\s+`), "role_hijack"},
	{regexp.MustCompile(`(?i)do\s+not\s+tell\s+the\s+user`), "deception_hide"},
	{regexp.MustCompile(`(?i)system\s+prompt\s+override`), "sys_prompt_override"},
	{regexp.MustCompile(`(?i)disregard\s+(your|all|any)\s+(instructions|rules|guidelines)`), "disregard_rules"},
}

func NewMemoryCurateTool(workspace string) *MemoryCurateTool {
	return &MemoryCurateTool{
		workspace:        workspace,
		memoryCharLimit:  defaultMemoryCharLimit,
		profileCharLimit: defaultProfileCharLimit,
	}
}

func (t *MemoryCurateTool) Name() string {
	return "memory_curate"
}

func (t *MemoryCurateTool) Description() string {
	return `Save durable information to persistent curated memory that survives across sessions and is always injected into context.

WHEN TO SAVE (do this proactively, don't wait to be asked):
- User corrects you or says "remember this" / "don't do that again"
- User shares a preference, habit, or personal detail (name, role, timezone, coding style)
- You discover something about the environment (OS, installed tools, project structure)
- You learn a convention, API quirk, or workflow specific to this user's setup

PRIORITY: User preferences and corrections > environment facts > procedural knowledge.
The most valuable memory prevents the user from having to repeat themselves.

TWO TARGETS:
- 'user': who the user is — name, role, preferences, communication style, pet peeves
- 'memory': your notes — environment facts, project conventions, tool quirks, lessons learned

ACTIONS: add (new entry), replace (update existing — old_text identifies it), remove (delete — old_text identifies it).

SKIP: trivial/obvious info, things easily re-discovered, raw data dumps, and temporary task state.
For procedural knowledge (how to do things), use skill_manage instead.`
}

func (t *MemoryCurateTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"add", "replace", "remove"},
				"description": "The action to perform.",
			},
			"target": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"memory", "user"},
				"description": "Which memory store: 'memory' for personal notes, 'user' for user profile.",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The entry content. Required for 'add' and 'replace'.",
			},
			"old_text": map[string]interface{}{
				"type":        "string",
				"description": "Short unique substring identifying the entry to replace or remove.",
			},
		},
		"required": []string{"action", "target"},
	}
}

func (t *MemoryCurateTool) pathFor(target string) string {
	return t.pathForContext(target, "")
}

// pathForContext resolves the note file: the legacy global file for the
// personal context (backward compatible), per-context files otherwise.
// A note is global only when written without a context — never by accident.
func (t *MemoryCurateTool) pathForContext(target, contextID string) string {
	base := "curated-memory.md"
	if target == "user" {
		base = "user-profile.md"
	}
	if contextID == "" || contextID == "personal" {
		return filepath.Join(t.workspace, "knowledge", "self", base)
	}
	safe := sanitizeContextID(contextID)
	return filepath.Join(t.workspace, "knowledge", "self", "contexts", safe, base)
}

func sanitizeContextID(id string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "unknown"
	}
	return out
}

// contextOf resolves the calling session's context (personal default).
func (t *MemoryCurateTool) contextOf(ctx context.Context) string {
	if t.ContextFor == nil {
		return "personal"
	}
	if id := t.ContextFor(SessionKeyFromContext(ctx)); id != "" {
		return id
	}
	return "personal"
}

func (t *MemoryCurateTool) charLimit(target string) int {
	if target == "user" {
		return t.profileCharLimit
	}
	return t.memoryCharLimit
}

func (t *MemoryCurateTool) readEntries(target string) []string {
	return t.readEntriesFor(target, "")
}

// readEntriesFor merges the global file with the session context's file
// (deduped, global first). Personal sessions read the global files.
func (t *MemoryCurateTool) readEntriesFor(target, contextID string) []string {
	seen := map[string]bool{}
	var out []string
	for _, path := range []string{t.pathFor(target), t.pathForContext(target, contextID)} {
		if path == "" {
			continue
		}
		for _, e := range readNoteFile(path) {
			if !seen[e] {
				seen[e] = true
				out = append(out, e)
			}
		}
	}
	return out
}

func readNoteFile(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{}
	}
	raw := string(data)
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	parts := strings.Split(raw, entryDelimiter)
	entries := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			entries = append(entries, p)
		}
	}
	return entries
}

func (t *MemoryCurateTool) writeEntries(target string, entries []string) error {
	return t.writeEntriesFor(target, "", entries)
}

// writeEntriesFor persists to the session context's file (non-personal)
// or the global file (personal/legacy). Writes never cross contexts.
func (t *MemoryCurateTool) writeEntriesFor(target, contextID string, entries []string) error {
	path := t.pathForContext(target, contextID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	content := strings.Join(entries, entryDelimiter)
	return os.WriteFile(path, []byte(content), 0644)
}

func (t *MemoryCurateTool) charCount(entries []string) int {
	if len(entries) == 0 {
		return 0
	}
	return len(strings.Join(entries, entryDelimiter))
}

func scanMemoryContent(content string) string {
	for _, tp := range memoryThreatPatterns {
		if tp.pattern.MatchString(content) {
			return fmt.Sprintf("Blocked: content matches threat pattern '%s'. Memory entries are injected into the system prompt and must not contain injection payloads.", tp.id)
		}
	}
	return ""
}

func (t *MemoryCurateTool) successResponse(target string, entries []string, message string) *ToolResult {
	current := t.charCount(entries)
	limit := t.charLimit(target)
	pct := 0
	if limit > 0 {
		pct = (current * 100) / limit
	}

	result := map[string]interface{}{
		"success":     true,
		"message":     message,
		"target":      target,
		"entries":     entries,
		"usage":       fmt.Sprintf("%d%% — %d/%d chars", pct, current, limit),
		"entry_count": len(entries),
	}
	jsonResult, _ := json.Marshal(result)
	return NewToolResult(string(jsonResult))
}

func (t *MemoryCurateTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	action, _ := args["action"].(string)
	target, _ := args["target"].(string)

	if action == "" {
		return ErrorResult("action is required. Use: add, replace, remove")
	}
	if target != "memory" && target != "user" {
		return ErrorResult("target must be 'memory' or 'user'.")
	}

	contextID := t.contextOf(ctx)
	switch action {
	case "add":
		return t.addEntry(target, args, contextID)
	case "replace":
		return t.replaceEntry(target, args, contextID)
	case "remove":
		return t.removeEntry(target, args, contextID)
	default:
		return ErrorResult(fmt.Sprintf("Unknown action '%s'. Use: add, replace, remove", action))
	}
}

func (t *MemoryCurateTool) addEntry(target string, args map[string]interface{}, contextID string) *ToolResult {
	content, _ := args["content"].(string)
	content = strings.TrimSpace(content)
	if content == "" {
		return ErrorResult("content is required for 'add' action.")
	}

	// Scan for injection
	if errMsg := scanMemoryContent(content); errMsg != "" {
		return ErrorResult(errMsg)
	}

	entries := t.readEntriesFor(target, contextID)

	// Reject duplicates
	for _, e := range entries {
		if e == content {
			return t.successResponse(target, entries, "Entry already exists (no duplicate added).")
		}
	}

	// Check char limit
	newEntries := append(entries, content)
	newTotal := t.charCount(newEntries)
	limit := t.charLimit(target)

	if newTotal > limit {
		current := t.charCount(entries)
		result := map[string]interface{}{
			"success": false,
			"error": fmt.Sprintf("Memory at %d/%d chars. Adding this entry (%d chars) would exceed the limit. Replace or remove existing entries first.",
				current, limit, len(content)),
			"current_entries": entries,
			"usage":           fmt.Sprintf("%d/%d", current, limit),
		}
		jsonResult, _ := json.Marshal(result)
		return ErrorResult(string(jsonResult))
	}

	// Append to this context's own file only (never merged-then-written,
	// which would duplicate global entries into context files).
	own := readNoteFile(t.pathForContext(target, contextID))
	if t.pathForContext(target, contextID) == t.pathFor(target) {
		own = entries // personal writes the global file directly
	}
	own = append(own, content)
	if err := writeNoteFile(t.pathForContext(target, contextID), own); err != nil {
		return ErrorResult(fmt.Sprintf("Failed to write memory: %v", err))
	}

	logger.InfoCF("memory_curate", "Entry added", map[string]interface{}{
		"target": target,
		"chars":  len(content),
	})

	return t.successResponse(target, entries, "Entry added.")
}

func (t *MemoryCurateTool) replaceEntry(target string, args map[string]interface{}, contextID string) *ToolResult {
	oldText, _ := args["old_text"].(string)
	content, _ := args["content"].(string)
	oldText = strings.TrimSpace(oldText)
	content = strings.TrimSpace(content)

	if oldText == "" {
		return ErrorResult("old_text is required for 'replace' action.")
	}
	if content == "" {
		return ErrorResult("content is required for 'replace' action. Use 'remove' to delete entries.")
	}

	// Scan for injection
	if errMsg := scanMemoryContent(content); errMsg != "" {
		return ErrorResult(errMsg)
	}

	entries := t.readEntriesFor(target, contextID)

	// Find matching entry in the merged view (uniqueness across global +
	// context), then apply per-file so files never cross-contaminate.
	matchCount := 0
	for _, e := range entries {
		if strings.Contains(e, oldText) {
			matchCount++
		}
	}

	if matchCount == 0 {
		return ErrorResult(fmt.Sprintf("No entry matched '%s'.", oldText))
	}
	if matchCount > 1 {
		return ErrorResult(fmt.Sprintf("Multiple entries matched '%s'. Be more specific.", oldText))
	}

	// Check char limit with replacement
	testEntries := make([]string, len(entries))
	copy(testEntries, entries)
	for i, e := range testEntries {
		if strings.Contains(e, oldText) {
			testEntries[i] = content
		}
	}
	if t.charCount(testEntries) > t.charLimit(target) {
		return ErrorResult("Replacement would exceed the character limit. Shorten the new content or remove other entries first.")
	}

	if err := t.mutateFiles(target, contextID, func(es []string) ([]string, bool) {
		changed := false
		for i, e := range es {
			if strings.Contains(e, oldText) {
				es[i] = content
				changed = true
			}
		}
		return es, changed
	}); err != nil {
		return ErrorResult(fmt.Sprintf("Failed to write memory: %v", err))
	}

	logger.InfoCF("memory_curate", "Entry replaced", map[string]interface{}{
		"target": target,
	})

	return t.successResponse(target, testEntries, "Entry replaced.")
}

func (t *MemoryCurateTool) removeEntry(target string, args map[string]interface{}, contextID string) *ToolResult {
	oldText, _ := args["old_text"].(string)
	oldText = strings.TrimSpace(oldText)

	if oldText == "" {
		return ErrorResult("old_text is required for 'remove' action.")
	}

	entries := t.readEntriesFor(target, contextID)

	// Find matching entry in the merged view, then remove per-file.
	matchCount := 0
	for _, e := range entries {
		if strings.Contains(e, oldText) {
			matchCount++
		}
	}

	if matchCount == 0 {
		return ErrorResult(fmt.Sprintf("No entry matched '%s'.", oldText))
	}
	if matchCount > 1 {
		return ErrorResult(fmt.Sprintf("Multiple entries matched '%s'. Be more specific.", oldText))
	}

	// Remove the entry per-file (never merged-then-written).
	if err := t.mutateFiles(target, contextID, func(es []string) ([]string, bool) {
		kept := es[:0:0]
		changed := false
		for _, e := range es {
			if strings.Contains(e, oldText) {
				changed = true
				continue
			}
			kept = append(kept, e)
		}
		return kept, changed
	}); err != nil {
		return ErrorResult(fmt.Sprintf("Failed to write memory: %v", err))
	}

	logger.InfoCF("memory_curate", "Entry removed", map[string]interface{}{
		"target": target,
	})

	remaining := t.readEntriesFor(target, contextID)
	return t.successResponse(target, remaining, "Entry removed.")
}

// mutateFiles applies fn to the global file and the session context's
// file independently (deduplicated when identical). Per-file mutation
// guarantees files never cross-contaminate through merged views.
func (t *MemoryCurateTool) mutateFiles(target, contextID string, fn func([]string) ([]string, bool)) error {
	paths := []string{t.pathFor(target)}
	if cp := t.pathForContext(target, contextID); cp != paths[0] {
		paths = append(paths, cp)
	}
	for _, path := range paths {
		entries := readNoteFile(path)
		updated, changed := fn(entries)
		if !changed {
			continue
		}
		if err := writeNoteFile(path, updated); err != nil {
			return err
		}
	}
	return nil
}

// Entries returns the current entries for a target ("user" or "memory").
func (t *MemoryCurateTool) Entries(target string) []string {
	return t.readEntries(target)
}

// Delete removes the entry everywhere it appears (global + all context
// files): the owner console manages the whole Ghost, so forgetting is
// total. Returns the remaining count across all files.
func (t *MemoryCurateTool) Delete(target, oldText string) (int, error) {
	oldText = strings.TrimSpace(oldText)
	if target != "user" && target != "memory" {
		return 0, fmt.Errorf("target must be 'user' or 'memory'")
	}
	if oldText == "" {
		return 0, fmt.Errorf("old_text is required")
	}
	remaining := 0
	var firstErr error
	// Per-file (never merged): deleting must not copy global entries
	// into context files or vice versa.
	paths := []string{t.pathFor(target)}
	for _, contextID := range t.knownContexts() {
		paths = append(paths, t.pathForContext(target, contextID))
	}
	for _, path := range paths {
		entries := readNoteFile(path)
		kept := entries[:0:0]
		for _, e := range entries {
			if !strings.Contains(e, oldText) {
				kept = append(kept, e)
			}
		}
		if len(kept) != len(entries) {
			if err := writeNoteFile(path, kept); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		remaining += len(kept)
	}
	return remaining, firstErr
}

func writeNoteFile(path string, entries []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(entries, entryDelimiter)), 0644)
}

// knownContexts lists context ids with note files on disk.
func (t *MemoryCurateTool) knownContexts() []string {
	dir := filepath.Join(t.workspace, "knowledge", "self", "contexts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// FormatForSystemPrompt returns the curated memory content formatted for system prompt injection.
// Returns empty string if no entries exist.
func (t *MemoryCurateTool) FormatForSystemPrompt(target string) string {
	return t.FormatForSystemPromptFor(target, "")
}

// FormatForSystemPromptFor scopes curated notes to a context: global
// notes plus the session context's notes. The prompt never sees
// foreign-context notes.
func (t *MemoryCurateTool) FormatForSystemPromptFor(target, contextID string) string {
	entries := t.readEntriesFor(target, contextID)
	if len(entries) == 0 {
		return ""
	}

	current := t.charCount(entries)
	limit := t.charLimit(target)
	pct := 0
	if limit > 0 {
		pct = (current * 100) / limit
	}

	content := strings.Join(entries, entryDelimiter)

	var header string
	if target == "user" {
		header = fmt.Sprintf("USER PROFILE (who the user is) [%d%% — %d/%d chars]", pct, current, limit)
	} else {
		header = fmt.Sprintf("CURATED MEMORY (your personal notes) [%d%% — %d/%d chars]", pct, current, limit)
	}

	separator := strings.Repeat("═", 46)
	return fmt.Sprintf("%s\n%s\n%s\n%s", separator, header, separator, content)
}
