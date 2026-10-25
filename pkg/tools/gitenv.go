package tools

import (
	"os"
	"path/filepath"
)

// Git inside the command sandbox has no terminal, runs as root over folders that
// belong to the owner, and starts with no identity. Left alone it fails in three
// ways the model cannot see coming: "detected dubious ownership" on any repo
// the owner owns, "Author identity unknown" on the first commit, and a prompt
// for a username that nothing can answer.

// gitSandboxEnv is the git environment every command gets. It is set at command
// level, so it never rewrites a repository's own configuration.
//
//   - safe.directory=* : (GIT_CONFIG_PARAMETERS, because the environment filter
//     rightly drops any variable named like a key) the sandbox shows one project, and its owner is the
//     person Ghost works for. Git's ownership check is for shared machines.
//   - GIT_TERMINAL_PROMPT=0 and GCM_INTERACTIVE=never : no credentials means
//     a clear failure at once, not a prompt that waits for a person who is not
//     there.
//   - GIT_EDITOR=true and PAGER=cat : nothing opens an editor or a pager.
func gitSandboxEnv() []string {
	return []string{
		"GIT_CONFIG_PARAMETERS='safe.directory=*'",
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
		"GIT_EDITOR=true",
		"PAGER=cat",
		"GIT_PAGER=cat",
	}
}

// gitIdentityConfig is the global git configuration Ghost starts with. Commits
// it makes are marked as Ghost's, so they are easy to tell from the owner's, and
// the owner can replace the file with their own.
const gitIdentityConfig = `[user]
	name = Ghost
	email = ghost@ghost.local
[init]
	defaultBranch = main
`

// ensureGitIdentity writes the starting global git configuration into the
// sandbox's home (the workspace scratch area) when there is none. It never
// replaces a file that is already there.
func ensureGitIdentity(workspace string) {
	if workspace == "" {
		return
	}
	dir := filepath.Join(workspace, "tmp")
	path := filepath.Join(dir, ".gitconfig")
	if _, err := os.Stat(path); err == nil {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	if err := os.WriteFile(path, []byte(gitIdentityConfig), 0o644); err == nil {
		inheritOwner(path)
	}
}
