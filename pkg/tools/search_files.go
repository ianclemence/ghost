package tools

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	searchDefaultLimit = 20
	searchMaxLimit     = 100
	searchMaxFileBytes = 2 << 20
	searchSnippetRunes = 200
)

var searchSkipDirs = map[string]bool{
	"skills": true, ".cache": true, "node_modules": true, ".git": true,
	"personal-context": true, "state": true, "memory": true, "sessions": true, "commitments": true,
	"pending": true, "events": true, "cron": true, "journal": true, "dreams": true, "proactive": true,
	"conversations": true, "tmp": true, "watches": true,
}

type SearchFilesTool struct {
	workspace string
	restrict  bool
	guard     ScopeGuard
}

func NewSearchFilesTool(workspace string, restrict bool) *SearchFilesTool {
	return &SearchFilesTool{workspace: workspace, restrict: restrict}
}

func (t *SearchFilesTool) SetScopeGuard(g ScopeGuard) { t.guard = g }

func (t *SearchFilesTool) Name() string { return "search_files" }

func (t *SearchFilesTool) Description() string {
	return "Find which of the owner's files mention a word or phrase, with the matching line. Use it for \"search my files for X\", \"which note mentions X\", \"where did I write X\". " +
		"Searches text files in the workspace, case-insensitive; never memory, sessions or other internal stores. Read the file with read_file afterwards if you need more than the line."
}

func (t *SearchFilesTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "The word or phrase to find"},
			"path":  map[string]interface{}{"type": "string", "description": "Folder to search, relative to the workspace. Default: the whole workspace."},
			"limit": map[string]interface{}{"type": "integer", "description": "Most matches to return (default 20, max 100)"},
		},
		"required": []string{"query"},
	}
}

func looksLikeText(f *os.File) bool {
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	for _, b := range buf[:n] {
		if b == 0 {
			return false
		}
	}
	_, _ = f.Seek(0, 0)
	return true
}

func (t *SearchFilesTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	query, _ := args["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return ErrorResult("query is required")
	}
	rootArg, _ := args["path"].(string)
	if rootArg == "" {
		rootArg = "."
	}
	limit := searchDefaultLimit
	if v, ok := args["limit"].(float64); ok && int(v) > 0 {
		limit = int(v)
	}
	if limit > searchMaxLimit {
		limit = searchMaxLimit
	}
	root, err := validatePath(rootArg, t.workspace, t.restrict)
	if err != nil {
		return ErrorResult(err.Error())
	}
	session := SessionKeyFromContext(ctx)
	needle := strings.ToLower(query)
	var out []string
	files := map[string]bool{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || ctx.Err() != nil || len(out) >= limit {
			return filepath.SkipAll
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (searchSkipDirs[name] || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			if t.guard.Check(session, OpList, p) != nil {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > searchMaxFileBytes || strings.HasPrefix(name, ".") {
			return nil
		}
		if t.guard.Check(session, OpRead, p) != nil {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer f.Close()
		if !looksLikeText(f) {
			return nil
		}
		rel, _ := filepath.Rel(t.workspace, p)
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for line := 1; sc.Scan(); line++ {
			text := sc.Text()
			if !strings.Contains(strings.ToLower(text), needle) {
				continue
			}
			r := []rune(strings.TrimSpace(text))
			if len(r) > searchSnippetRunes {
				r = append(r[:searchSnippetRunes], '…')
			}
			out = append(out, fmt.Sprintf("%s:%d: %s", rel, line, string(r)))
			files[rel] = true
			if len(out) >= limit {
				break
			}
		}
		return nil
	})
	if len(out) == 0 {
		return NewToolResult(fmt.Sprintf("No file in %s mentions %q.", rootArg, query))
	}
	head := fmt.Sprintf("%d match(es) in %d file(s) for %q", len(out), len(files), query)
	if len(out) >= limit {
		head += fmt.Sprintf(" (stopped at %d; narrow the query or the path for more)", limit)
	}
	return NewToolResult(head + ":\n" + strings.Join(out, "\n"))
}
