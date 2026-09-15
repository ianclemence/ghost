// Package nontty enforces the non-interactive CLI rule: commands that
// would prompt must instead fail early with a descriptive error when
// stdin is not a terminal (no TTY, or CI=1). Task runners, updaters,
// and agents get a clear failure instead of a hang on a swallowed
// prompt.
package nontty

import (
	"fmt"
	"os"
)

// Interactive reports whether stdin is a terminal and CI mode is off.
func Interactive() bool {
	if os.Getenv("CI") != "" {
		return false
	}
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// RequireInteractive errors when a prompt would otherwise appear. Call
// it before any Scanln/readline/password read outside explicitly
// interactive commands, naming the flag that answers non-interactively.
func RequireInteractive(what, flag string) error {
	if Interactive() {
		return nil
	}
	if flag != "" {
		return fmt.Errorf("%s needs input but stdin is not a terminal; re-run with %s", what, flag)
	}
	return fmt.Errorf("%s needs input but stdin is not a terminal; refusing to prompt", what)
}
