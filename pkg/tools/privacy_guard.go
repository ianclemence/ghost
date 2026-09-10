package tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ScopeGuard enforces Ghost's memory privacy boundary at the file-tool
// layer.
//
// Ghost's memory is governed state with its own context/scope rules. Every
// retrieval path (digest, curated notes, recall, RAG) filters through
// those rules before anything reaches the model. Raw file access must
// never become a way to bypass that filter: a model reading the memory
// journal directly could pull facts out of a context it is not authorized
// to see. The ScopeGuard denies file tools access to the parts of the
// workspace that hold scoped memory state, forcing the model onto the
// governed memory tools (which are scope-filtered).
//
// It is installed by the agent runtime when a context store is present.
// With no resolver installed it is inert and file tools behave exactly as
// before (workspace confinement only) — backward compatible for unwired
// loops and non-context workspaces.
type ScopeGuard struct {
	// Workspace is the agent workspace root used to compute the relative
	// location of a requested path.
	Workspace string
	// ContextOf resolves a session's context id ("personal" default). A
	// nil resolver disables the guard.
	ContextOf func(sessionKey string) string
}

func (g ScopeGuard) active() bool {
	return g.ContextOf != nil && g.Workspace != ""
}

// Op describes what a file tool wants to do: "read" (read_file, edit_file
// which reads before replacing), "write" (write_file, append_file), or
// "list" (list_dir).
type Op string

const (
	OpRead  Op = "read"
	OpWrite Op = "write"
	OpList  Op = "list"
)

// Check returns an error when the resolved path lives inside a protected
// part of the memory estate that the calling session is not allowed to
// touch in the requested way. A nil return means the access is permitted.
// Reads of mixed-scope journals are denied (use the scope-filtered memory
// tools); writes stay permitted so journaling and user note-taking keep
// working, and listings reveal only names.
func (g ScopeGuard) Check(sessionKey string, op Op, resolvedPath string) error {
	if !g.active() {
		return nil
	}
	absWs, err := filepath.Abs(g.Workspace)
	if err != nil {
		return nil
	}
	absPath, err := filepath.Abs(resolvedPath)
	if err != nil {
		return nil
	}
	if !under(absWs, absPath) {
		return nil
	}
	rel, err := filepath.Rel(absWs, absPath)
	if err != nil {
		return nil
	}
	rel = filepath.ToSlash(rel)
	ctx := ""
	if g.ContextOf != nil {
		ctx = g.ContextOf(sessionKey)
	}
	switch {
	// The structured memory journal holds every context's entries in one
	// append-only file. It is internal governed state — the model reaches
	// it through scope-filtered memory tools, never by raw file reads.
	case rel == "personal-context" || strings.HasPrefix(rel, "personal-context/"):
		return fmt.Errorf("your memory store is protected: use your memory tools rather than reading it directly")
	// Dated daily notes are the same kind of mixed-scope journal: the
	// auto-journal appends every context's turn summaries to one shared
	// file, so no single session may read it raw. memory_recall serves
	// the same content scope-filtered. MEMORY.md (the user's own
	// long-term file, never machine-journaled) stays directly readable.
	case rel == "memory" || strings.HasPrefix(rel, "memory/"):
		if rel != "memory/MEMORY.md" && op == OpRead {
			return fmt.Errorf("dated memory notes hold every context's journal mixed together: use memory_recall to search them instead of reading files directly")
		}
	// Runtime state (identity, context/session bindings, browser profiles)
	// is never model content.
	case rel == "state" || strings.HasPrefix(rel, "state/"):
		return fmt.Errorf("that internal state is protected")
	// Per-context curated notes are visible only inside their own context.
	// The global files directly under knowledge/self/ remain shared.
	case strings.HasPrefix(rel, "knowledge/self/contexts/"):
		rest := strings.TrimPrefix(rel, "knowledge/self/contexts/")
		dirCtx := rest
		if idx := strings.Index(rest, "/"); idx >= 0 {
			dirCtx = rest[:idx]
		}
		if dirCtx != "" && dirCtx != ctx {
			return fmt.Errorf("those notes belong to another context and are not visible here")
		}
	}
	return nil
}

// ProtectedStoreTokens are path fragments of the internal memory estate
// that shell execution must not be used to reach either. They are matched
// as whole-word tokens against a command string, so legitimate commands
// that never touch the memory estate are unaffected.
var ProtectedStoreTokens = []string{
	"personal-context",
	"entries.jsonl",
	"session-context.json",
	"knowledge/self/contexts",
}

// execDeniedByPrivacy reports whether a shell command references the
// protected memory estate. Command-shape analysis is intentionally
// heuristic: it is a second layer behind the file-tool guard, not a
// substitute for it.
func execDeniedByPrivacy(command string) (string, bool) {
	for _, tok := range ProtectedStoreTokens {
		if strings.Contains(command, tok) {
			return fmt.Sprintf("that command touches a protected memory store (%s); use your memory tools instead", tok), true
		}
	}
	return "", false
}
