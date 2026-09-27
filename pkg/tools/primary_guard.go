package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Primary-file integrity guard.
//
// Ghost's primary files are the artifacts whose destruction bricks the
// system or silently destroys the owner's data: the SQLite database, the
// prompt contract files (GHOST.md and its companions), the skills estate,
// and the long-term memory digest. The file tools are the model's CRUD
// surface; this guard makes overwrite-and-truncate of those files
// impossible through them, independently of the workspace-restriction
// setting (integrity is not a config option). The application's own
// in-process writers — database driver, remember, skill_manage, updaters
// — never route through these tools, so legitimate maintenance keeps
// working.

// contractFiles are the shipped identity/prompt files: the model has
// sanctioned surfaces (skill_manage, its own memory tools) for behavior
// and memory, never raw rewrites of the contract itself.
var contractFiles = []string{"AGENTS.md", "SOUL.md", "IDENTITY.md", "HEARTBEAT.md"}

// guardPrimaryFile returns an error when resolvedPath names — or lives
// inside — a Ghost primary file. It is checked by every write-class file
// tool path (write, edit, append, frame output) before any byte lands on
// disk.
func guardPrimaryFile(resolvedPath string) error {
	clean := filepath.Clean(resolvedPath)
	base := filepath.Base(clean)

	// The database and its journal/backup family: any overwrite is
	// immediate, unrecoverable data loss.
	if strings.HasPrefix(base, "ghost.db") {
		return primaryDeny(base, "the database")
	}
	// The governance prompt ships as GHOST.md; backups keep the prefix.
	if strings.HasPrefix(base, "GHOST.md") {
		return primaryDeny(base, "the prompt contract")
	}
	for _, name := range contractFiles {
		if base == name {
			return primaryDeny(name, "the identity contract")
		}
	}
	// The long-term memory digest is regenerated from the canonical store;
	// a raw overwrite would destroy the owner's memory history.
	if base == "MEMORY.md" && hasDirSegment(clean, "memory") {
		return primaryDeny("memory/MEMORY.md", "the long-term memory digest")
	}
	// The skills estate is Ghost's procedural behavior; skill_manage is
	// the only sanctioned path and it is broker-gated.
	if hasDirSegment(clean, "skills") {
		return primaryDeny("skills/", "the skills estate")
	}
	return nil
}

func primaryDeny(name, what string) error {
	return fmt.Errorf("access denied: %s is a Ghost primary file (%s); primary files are never overwritten or deleted through file tools", name, what)
}

func hasDirSegment(p, seg string) bool {
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == seg {
			return true
		}
	}
	return false
}

// execDeniedPrimaryFiles is the shell-side second layer behind
// guardPrimaryFile: command-shape analysis, intentionally fail-closed.
// The database and config are blanket-denied — no model-initiated command
// has any legitimate reason to name them at all. Everything else is
// denied only in a destructive context, so ordinary reads and listings
// keep working.
func execDeniedPrimaryFiles(command string) (string, bool) {
	// Blanket: naming these from the shell is never legitimate.
	blanket := []string{"ghost.db"}
	if dir := strings.TrimSpace(os.Getenv("GHOST_CONFIG_DIR")); dir != "" {
		blanket = append(blanket, dir)
	} else {
		// Install convention when the service env is not exported here.
		blanket = append(blanket, "/var/ghost/config")
	}
	for _, tok := range blanket {
		if strings.Contains(command, tok) {
			return fmt.Sprintf("that command names a Ghost primary file (%s); the database and config are never touched from the shell — use Ghost's own tools instead", tok), true
		}
	}

	// Destructive context: a write-shaped command may never reference a
	// primary file or a top-level estate path.
	if hasDestructiveShape(command) {
		if name, ok := primaryNameIn(command); ok {
			return fmt.Sprintf("that command would destroy a Ghost primary file (%s); primary files are never deleted or modified from the shell", name), true
		}
	}
	// Broad recursive wipe of whatever directory the command runs in.
	if recursiveWipeOfTree(command) {
		return "that command would recursively wipe the working tree, which contains Ghost's primary files; primary files are never deleted", true
	}
	return "", false
}

// destructiveVerbRe matches the command words that remove, move, or
// overwrite files. Word-bounded so "curl", "format", or "add" never trip it.
var destructiveVerbRe = regexp.MustCompile(`(^|[^a-zA-Z0-9])(rm|rmdir|shred|unlink|truncate|mv|cp|tee|dd|sed|ln|install)([^a-zA-Z0-9]|$)`)

// destructiveRedirectRe matches shell output redirection: any redirect in
// the same command as a primary name is treated as write-shaped.
var destructiveRedirectRe = regexp.MustCompile(`[0-9]?>`)

// inlineScriptRe matches interpreter inline-execution flags — the one
// shape where destruction hides behind an arbitrary language call.
var inlineScriptRe = regexp.MustCompile(`(python3?|node|perl|ruby)\s+(-[ce]\b|--command\b|--eval\b)`)

func hasDestructiveShape(command string) bool {
	if destructiveVerbRe.MatchString(command) {
		return true
	}
	if destructiveRedirectRe.MatchString(command) {
		return true
	}
	// find … -delete is deletion however it is spelled.
	if strings.Contains(command, "-delete") {
		return true
	}
	// python -c / node -e style one-liners can remove anything; treated as
	// destructive whenever a primary name appears alongside.
	if inlineScriptRe.MatchString(command) {
		return true
	}
	return false
}

// primaryFragments are the path fragments whose appearance in a
// destructive command means refusal: contract files, the memory digest,
// and the top-level workspace estate directories. Fragment matching is
// deliberately conservative (a name like "skills-backup" still trips
// "skills"): fail closed, never fail open.
var primaryFragments = []string{
	"GHOST.md", "AGENTS.md", "SOUL.md", "IDENTITY.md", "HEARTBEAT.md", "MEMORY.md",
	"skills", "knowledge", "personal-context", "state", "commitments",
	"sessions", "cron", "dreams", "proactive", "events", "journal", "workspace",
	"/usr/local/bin/ghost", "/.local/bin/ghost",
}

func primaryNameIn(command string) (string, bool) {
	for _, frag := range primaryFragments {
		if strings.Contains(command, frag) {
			return frag, true
		}
	}
	return "", false
}

// rmTailRe captures everything after an rm/rmdir invocation for the
// broad-wipe check below.
var rmTailRe = regexp.MustCompile(`(^|[^a-zA-Z0-9])(rm|rmdir)([^a-zA-Z0-9]|$)([^;&|]*)`)

// recursiveFlagRe matches -r/-R/--recursive style flags.
var recursiveFlagRe = regexp.MustCompile(`(^|\s)-{1,2}[a-zA-Z-]*[rR]`)

// broadTreeTargets are the standing targets that mean "everything here".
var broadTreeTargets = map[string]bool{
	".": true, "./": true, "..": true, "*": true, "./*": true,
}

// recursiveWipeOfTree reports whether the command recursively removes the
// whole working tree (rm -rf . and friends) — the one shape that destroys
// every primary file without naming any of them.
func recursiveWipeOfTree(command string) bool {
	m := rmTailRe.FindStringSubmatch(command)
	if m == nil {
		return false
	}
	tail := m[4]
	if !recursiveFlagRe.MatchString(tail) {
		return false
	}
	for _, field := range strings.Fields(tail) {
		if broadTreeTargets[field] {
			return true
		}
	}
	return false
}
